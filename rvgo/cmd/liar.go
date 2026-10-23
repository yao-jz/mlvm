package cmd

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/internal/e2edemo"
	"github.com/yao-jz/mlvm/rvgo/internal/liar"
	"github.com/yao-jz/mlvm/rvgo/internal/xray"
)

// LiarReport is the machine-readable result of `mlvm liar`: the record of the dispute,
// which `mlvm referee` checks again.
type LiarReport struct {
	Format string `json:"format"` // liar.RecordFormat
	Guest  string `json:"guest"`
	Prompt string `json:"prompt,omitempty"`
	Lie    string `json:"lie"`
	// HonestText and LiarText are what each machine answered: the generated text for the
	// decoder, otherwise the line of stdout the lie targets, or the last line printed.
	HonestText string `json:"honestText"`
	LiarText   string `json:"liarText"`
	// HonestCommitted and LiarCommitted are the answers the machines committed at MLVM_OUT
	// (fast.MLResult), as text when printable and hex otherwise, for guests that commit one.
	HonestCommitted string         `json:"honestCommitted,omitempty"`
	LiarCommitted   string         `json:"liarCommitted,omitempty"`
	Attempts        []liar.Attempt `json:"attempts,omitempty"`
	*liar.Result
	Seconds float64 `json:"seconds"`
}

func parseAt(s string, total uint64) (uint64, error) {
	if strings.HasSuffix(s, "%") {
		pct, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		if err != nil || pct < 0 || pct > 100 {
			return 0, fmt.Errorf("--at %q: want a step number or a percentage like 50%%", s)
		}
		return uint64(pct / 100 * float64(total)), nil
	}
	return strconv.ParseUint(strings.ReplaceAll(s, ",", ""), 10, 64)
}

// answerLine returns the line of out that contains byte offset at. When at is negative it
// returns the generated text of a guest that follows the decoder's output protocol
// ("generated: ...", then "tokens: ..."), or else the last non-empty line.
func answerLine(out []byte, at int) string {
	if at < 0 {
		if i := bytes.Index(out, []byte("generated: ")); i >= 0 {
			text := out[i+len("generated: "):]
			if j := bytes.Index(text, []byte("\ntokens:")); j >= 0 {
				return string(text[:j])
			}
		}
		trimmed := bytes.TrimRight(out, "\n")
		at = len(trimmed) - 1
		if at < 0 {
			return ""
		}
	}
	start := bytes.LastIndexByte(out[:at], '\n') + 1
	end := bytes.IndexByte(out[at:], '\n')
	if end < 0 {
		end = len(out)
	} else {
		end += at
	}
	return string(out[start:end])
}

// parseForge reads --forge OLD=NEW: two strings, or two 0x-prefixed hex byte strings, of
// the same length.
func parseForge(s string) (old, forged []byte, err error) {
	a, b, ok := strings.Cut(s, "=")
	if ok && strings.HasPrefix(a, "0x") && strings.HasPrefix(b, "0x") {
		if old, err = hex.DecodeString(a[2:]); err == nil {
			forged, err = hex.DecodeString(b[2:])
		}
		if err != nil {
			return nil, nil, fmt.Errorf("--forge %q: %w", s, err)
		}
	} else {
		old, forged = []byte(a), []byte(b)
	}
	if !ok || len(old) == 0 || len(old) != len(forged) {
		return nil, nil, fmt.Errorf("--forge needs OLD=NEW, two strings (or 0x-prefixed hex) of the same length, such as --forge 0.61=0.91")
	}
	return old, forged, nil
}

// committedText renders an answer committed at MLVM_OUT: as text when it is printable, as
// hex otherwise.
func committedText(payload []byte) string {
	for _, c := range payload {
		if c < 0x20 || c > 0x7e {
			return "0x" + hex.EncodeToString(payload)
		}
	}
	return string(payload)
}

// functionFilter accepts the program counters of the functions named fn: exactly, or as the
// last segment of a Rust path (__muldf3 matches compiler_builtins::float::mul::__muldf3).
func functionFilter(sym *xray.Symbolizer, fn string) (func(pc uint64) bool, error) {
	match := map[int]bool{}
	for i := 0; i < sym.Len(); i++ {
		if name := sym.Name(i); name == fn || strings.HasSuffix(name, "::"+fn) {
			match[i] = true
		}
	}
	if len(match) == 0 {
		return nil, fmt.Errorf("--function %q: no function by that name in the ELF", fn)
	}
	return func(pc uint64) bool { return match[sym.Lookup(pc)] }, nil
}

// progressLine shows how far a long run has got, on stderr when it is a terminal.
func progressLine(quiet bool) func(name string) func(step uint64) {
	fi, err := os.Stderr.Stat()
	if quiet || err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return func(string) func(uint64) { return nil }
	}
	return func(name string) func(uint64) {
		return func(step uint64) {
			fmt.Fprintf(os.Stderr, "\r  running %-7s %15s instructions\x1b[K", name, xrayGroup(step))
		}
	}
}

func clearProgress(quiet bool) {
	if fi, err := os.Stderr.Stat(); !quiet && err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprint(os.Stderr, "\r\x1b[K")
	}
}

func LiarAction(ctx *cli.Context) error {
	start := time.Now()
	// Both parties keep a large heap that lives until the end: guest memory pages and
	// checkpoints. Collecting at 25% growth instead of Go's default 100% cuts peak memory
	// from 4.3 GB to 2.6 GB on the Candle MiniLM guest, in the same time, because there
	// is little garbage to find. GOGC in the environment still wins.
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(25)
	}
	g, err := loadToolGuest(ctx)
	if err != nil {
		return err
	}
	decoder := g.Demo == "decoder"
	sym, err := xray.NewSymbolizer(g.ELF)
	if err != nil {
		return err
	}
	w := ctx.App.Writer
	quiet := ctx.Bool("json")
	say := func(format string, args ...any) {
		if !quiet {
			fmt.Fprintf(w, format, args...)
		}
	}
	progress := progressLine(quiet)
	const tickEvery = 1 << 24

	kind := liar.FaultKind(ctx.String("lie"))
	switch kind {
	case liar.FaultULP, liar.FaultLoad, liar.FaultInt, liar.FaultOutput, liar.FaultCommit:
	case liar.FaultToken:
		if !g.TinyStories && ctx.Uint("token-id") == 0 {
			return fmt.Errorf("--lie token: this guest's tokens are not TinyStories words; name the forged token with --token-id")
		}
	default:
		return fmt.Errorf("--lie %q: want ulp, load, int, output, commit or token", kind)
	}

	interval := ctx.Uint64("checkpoint-interval")
	honest := liar.NewParty("honest", g.State, nil, interval)
	var honestOut bytes.Buffer
	honest.SetStdout(&honestOut)
	if tick := progress("honest"); tick != nil {
		honest.SetTick(tickEvery, tick)
	}
	say("MLVM  ·  Catch the Liar\n\n")
	if decoder {
		say("  Two machines run the same TinyStories prompt %q. One of them lies once.\n\n", g.Prompt)
	} else {
		say("  Two machines run the same program (%s). One of them lies once.\n\n", g.Label)
	}
	if err := honest.Run(ctx.Uint64("max-steps"), nil); err != nil {
		return err
	}
	clearProgress(quiet)

	// stories: the answer is TinyStories token IDs at MLVM_OUT, shown as text.
	stories := g.TinyStories
	var vocab *e2edemo.Vocab
	var honestTokens []uint16
	if stories {
		if vocab, err = e2edemo.LoadVocab(); err != nil {
			return err
		}
		if honestTokens, err = e2edemo.ResultTokens(honest.Final()); err != nil {
			if decoder || kind == liar.FaultToken {
				return err
			}
			stories = false // it stages the tokenizer but commits something else
		}
	}

	fault := liar.Fault{Kind: kind}
	answerAt := -1 // the stdout offset the answer line is taken from
	switch fault.Kind {
	case liar.FaultULP, liar.FaultLoad, liar.FaultInt:
		at, err := parseAt(ctx.String("at"), honest.Steps())
		if err != nil {
			return err
		}
		fault.AtStep, fault.Mnemonic = at, ctx.String("mnemonic")
		if fn := ctx.String("function"); fn != "" {
			in, err := functionFilter(sym, fn)
			if err != nil {
				return err
			}
			fault.InFunction, fault.Function = in, fn
		}
		fault.Precision = ctx.String("precision")
		if fault.Precision == "" && !decoder {
			fault.Precision = "any"
		}
		switch fault.Precision {
		case "", "s", "d", "any":
		default:
			return fmt.Errorf("--precision %q: want s, d or any", fault.Precision)
		}
	case liar.FaultToken:
		if !stories {
			// Any guest that commits token IDs in the decoder's layout.
			if honestTokens, err = e2edemo.ResultTokens(honest.Final()); err != nil {
				return fmt.Errorf("--lie token: the guest commits no tokens in the decoder's layout: %w", err)
			}
		}
		fault.TokenIndex = ctx.Int("token-index")
		if fault.TokenIndex < 0 || fault.TokenIndex >= len(honestTokens) {
			return fmt.Errorf("--token-index %d: the answer has %d tokens", fault.TokenIndex, len(honestTokens))
		}
		if id := ctx.Uint("token-id"); id != 0 {
			if id > 0xffff {
				return fmt.Errorf("--token-id %d does not fit the committed u16 layout", id)
			}
			fault.Token = uint16(id)
			break
		}
		word := ctx.String("word")
		id, ok := vocab.Lookup(" " + strings.TrimSpace(word))
		if !ok {
			if id, ok = vocab.Lookup(word); !ok {
				return fmt.Errorf("--word %q is not a single token of the 512-token TinyStories vocabulary", word)
			}
		}
		fault.Token = id
	case liar.FaultOutput:
		old, forged, err := parseForge(ctx.String("forge"))
		if err != nil {
			return err
		}
		i := bytes.Index(honestOut.Bytes(), old)
		if i < 0 {
			return fmt.Errorf("--forge: the honest machine never printed %q", old)
		}
		fault.OutputOffset, fault.Output = uint64(i), forged
		answerAt = i
	case liar.FaultCommit:
		old, forged, err := parseForge(ctx.String("forge"))
		if err != nil {
			return err
		}
		payload, ok := fast.MLResult(honest.Final())
		if !ok {
			return fmt.Errorf("--lie commit: this guest commits no answer at MLVM_OUT (see examples/mlvm_result.h); --lie output forges what it prints instead")
		}
		i := bytes.Index(payload, old)
		if i < 0 {
			return fmt.Errorf("--forge: the committed answer %s does not contain %q", committedText(payload), old)
		}
		fault.OutputOffset, fault.Output = uint64(i), forged
	}

	// A lie can derail the program; past this, the liar treats the run as crashed.
	fault.StepLimit = 2*honest.Steps() + 1<<20
	var liarOut *bytes.Buffer
	setup := func(p *liar.Party) {
		liarOut = new(bytes.Buffer)
		p.SetStdout(liarOut)
		if tick := progress("liar"); tick != nil {
			p.SetTick(tickEvery, tick)
		}
	}
	dishonest, err := liar.RunUntilDisputed("liar", g.State, fault, interval, honest.FinalHash(), 5, setup)
	if err != nil {
		return err
	}
	clearProgress(quiet)

	report := &LiarReport{
		Format:   liar.RecordFormat,
		Guest:    g.Label,
		Prompt:   g.Prompt,
		Lie:      fault.String(),
		Attempts: dishonest.Attempts(),
	}
	if stories {
		liarTokens, err := e2edemo.ResultTokens(dishonest.Final())
		if err != nil {
			return err
		}
		report.HonestText, report.LiarText = vocab.Text(honestTokens), vocab.Text(liarTokens)
	} else {
		report.HonestText, report.LiarText = answerLine(honestOut.Bytes(), answerAt), answerLine(liarOut.Bytes(), answerAt)
		if payload, ok := fast.MLResult(honest.Final()); ok {
			report.HonestCommitted = committedText(payload)
		}
		if payload, ok := fast.MLResult(dishonest.Final()); ok {
			report.LiarCommitted = committedText(payload)
		}
	}
	name := func(pc uint64) string { return sym.Name(sym.Lookup(pc)) }

	if decoder {
		say("  honest  %q\n          poststate %s\n", g.Prompt+report.HonestText, honest.FinalHash().Hex())
		say("  liar    %q\n          poststate %s\n", g.Prompt+report.LiarText, dishonest.FinalHash().Hex())
	} else if stories {
		say("  honest  commits %q\n          %s instructions, poststate %s\n", report.HonestText, xrayGroup(honest.Steps()), honest.FinalHash().Hex())
		say("  liar    commits %q\n          %s instructions, poststate %s\n", report.LiarText, xrayGroup(dishonest.Steps()), dishonest.FinalHash().Hex())
	} else {
		show := func(who, text, committed string, steps uint64, post string) {
			if strings.ContainsAny(text, "\n\r") {
				text = strconv.Quote(text)
			}
			say("  %-6s  %s\n", who, text)
			if committed != "" {
				say("          commits %s\n", committed)
			}
			say("          %s instructions, poststate %s\n", xrayGroup(steps), post)
		}
		show("honest", report.HonestText, report.HonestCommitted, honest.Steps(), honest.FinalHash().Hex())
		show("liar", report.LiarText, report.LiarCommitted, dishonest.Steps(), dishonest.FinalHash().Hex())
	}
	if dev := dishonest.Deviation(); dev != nil {
		switch dev.Kind {
		case liar.FaultULP, liar.FaultLoad, liar.FaultInt:
			erased, vanished, faulted := 0, 0, 0
			for _, a := range report.Attempts {
				switch {
				case a.Faulted:
					faulted++
				case a.Vanished:
					vanished++
				case !a.Kept:
					erased++
				}
			}
			what := map[liar.FaultKind]string{
				liar.FaultULP:  "one-ULP lie(s)",
				liar.FaultLoad: "one-bit misread(s)",
				liar.FaultInt:  "one-bit integer lie(s)",
			}[dev.Kind]
			cause := "later rounding"
			if dev.Kind == liar.FaultInt {
				cause = "later instructions"
			}
			switch {
			case faulted > 0:
				say("\n  The liar's first %d %s did not stick: %d vanished (%s made the two machines\n", erased+vanished+faulted, what, erased+vanished, cause)
				say("  bit-identical again) and %d crashed the program. Lie #%d stuck:\n", faulted, erased+vanished+faulted+1)
			case erased+vanished > 0:
				say("\n  The liar's first %d %s vanished: %s made the two machines\n", erased+vanished, what, cause)
				say("  bit-identical again, so there was nothing left to catch. Lie #%d stuck:\n", erased+vanished+1)
			default:
				say("\n  The lie:\n")
			}
			say("    step %s  %s  in %s\n", xrayGroup(dev.Step), dev.Instruction, name(dev.PC))
			say("    %s = %s, kept as %s\n", dev.Location, dev.Honest, dev.Forged)
		case liar.FaultToken:
			say("\n  The lie: step %s  %s  in %s writes %s instead of %s at %s\n",
				xrayGroup(dev.Step), dev.Instruction, name(dev.PC), dev.Forged, dev.Honest, dev.Location)
		case liar.FaultCommit:
			say("\n  The lie: step %s  %s  in %s stores %s instead of %s at %s\n",
				xrayGroup(dev.Step), dev.Instruction, name(dev.PC), dev.Forged, dev.Honest, dev.Location)
		case liar.FaultOutput:
			say("\n  The lie: after step %s  %s  in %s,\n    memory at %s holds %s instead of %s\n",
				xrayGroup(dev.Step), dev.Instruction, name(dev.PC), dev.Location, dev.Forged, dev.Honest)
		}
	}
	if report.HonestText == report.LiarText && report.HonestCommitted == report.LiarCommitted {
		say("\n  Same answer, different machine state. Reading the output would never catch this.\n")
	}

	res, err := liar.Play(honest, dishonest, nil, name)
	if err != nil {
		return err
	}
	report.Result = res
	report.Seconds = time.Since(start).Seconds()
	if quiet {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	if res.NoDispute {
		say("\n  Both machines halted in the same state: the lie left no trace, so there is nothing to dispute.\n")
		return nil
	}

	total := max(res.HonestSteps, res.LiarSteps)
	say("\n  Bisection: the referee asks both machines for their state hash at a midpoint, %d times.\n\n", len(res.Rounds))
	for _, r := range res.Rounds {
		mark := "agree   → the lie is later"
		if !r.Agree {
			mark = "differ  → the lie is earlier"
		}
		say("    %2d  %s  %15s left  ask step %15s  %s\n", r.N, timeline(r.Lo, r.Hi, total, 24), xrayGroup(r.Hi-r.Lo), xrayGroup(r.Mid), mark)
	}
	printVerdict(w, res.Verdict)
	say("\n  %d questions narrowed %s instructions to one. %.1f s.\n", len(res.Rounds), xrayGroup(total), report.Seconds)
	return nil
}

// timeline draws where the disputed interval sits in the whole trace.
func timeline(lo, hi, total uint64, width int) string {
	a := int(float64(lo) / float64(total) * float64(width))
	b := int(float64(hi)/float64(total)*float64(width) + 0.999)
	if b <= a {
		b = a + 1
	}
	if b > width {
		b = width
	}
	return "[" + strings.Repeat("·", a) + strings.Repeat("█", b-a) + strings.Repeat("·", width-b) + "]"
}

func printVerdict(w io.Writer, v *liar.Verdict) {
	fmt.Fprintf(w, "\n  The disputed instruction\n")
	fmt.Fprintf(w, "    step %s, pc 0x%x: %s  (in %s)\n", xrayGroup(v.Step), v.PC, v.Instruction, v.Function)
	fmt.Fprintf(w, "    both agree on the state before it   %s\n", v.Agreed.Hex())
	fmt.Fprintf(w, "    honest claims after it               %s\n", v.HonestPost.Hex())
	fmt.Fprintf(w, "    liar claims after it                 %s\n", v.LiarPost.Hex())
	fmt.Fprintf(w, "    referee computes                     %s  → %s wins\n", v.RefereePost.Hex(), v.Winner)
	if v.EVM != nil {
		agree := "agrees"
		if !v.EVM.Matches {
			agree = "DISAGREES"
		}
		fmt.Fprintf(w, "    RISCV.sol in an EVM returns          %s  (%s, %s gas)\n", v.EVM.Post.Hex(), agree, xrayGroup(v.EVM.GasUsed))
	}
	fmt.Fprintf(w, "\n    checked by %s\n", v.Method)
	fmt.Fprintf(w, "    evidence: %d-byte state witness + %d bytes of Merkle proof\n", (len(v.StateWitness)-2)/2, v.ProofBytes)
	fmt.Fprintf(w, "    boundary: %s\n", v.Boundary)
}

var LiarCommand = &cli.Command{
	Name:      "liar",
	Usage:     "Catch the Liar: find one lied-about instruction among millions or billions by bisection",
	ArgsUsage: "[prompt]",
	Description: "Runs a guest twice: the embedded TinyStories decoder by default, the embedded MLP " +
		"(--demo mlp), or any RV64IMAFD ELF (--elf, with --load-file inputs). The second machine lies " +
		"once: it rounds one floating-point result one ULP the wrong way (--lie ulp), misreads one bit " +
		"of one value loaded from memory (--lie load), flips one bit of an integer result (--lie int), prints forged bytes (--lie output --forge OLD=NEW), " +
		"forges the answer the guest commits at MLVM_OUT (--lie commit --forge OLD=NEW), or, for the " +
		"decoder or another guest that commits TinyStories tokens, forges one of them (--lie token). A referee bisects on " +
		"the two machines' state hashes and checks the single instruction they disagree on. This is a " +
		"local simulation of an optimistic dispute: no chain, bonds or clocks.",
	Action: LiarAction,
	Flags: append(guestFlags("Run"),
		&cli.StringFlag{Name: "lie", Usage: "ulp (one ULP on a float result), load (one bit of a float load), int (one bit of an integer result), output (forge printed bytes), commit (forge the committed answer) or token (TinyStories guests: forge a committed token)", Value: "ulp"},
		&cli.StringFlag{Name: "at", Usage: "ulp, load, int: lie at the first matching instruction at or after this step or percentage", Value: "50%"},
		&cli.StringFlag{Name: "mnemonic", Usage: "ulp, load, int: only lie about this instruction, e.g. fadd.s, fmul.d, flw or add"},
		&cli.StringFlag{Name: "function", Usage: "ulp, load, int: only lie inside this function (a name, or the last segment of a Rust path)"},
		&cli.StringFlag{Name: "precision", Usage: "ulp, load: s (f32), d (f64) or any (default: s for the decoder, any otherwise)"},
		&cli.StringFlag{Name: "forge", Usage: "output, commit: OLD=NEW, print or commit NEW where the honest machine has OLD (same length; 0x-prefixed hex for binary answers)"},
		&cli.IntFlag{Name: "token-index", Usage: "token: which generated token to forge", Value: 9},
		&cli.StringFlag{Name: "word", Usage: "token: the forged word (one TinyStories vocabulary token)", Value: "Tim"},
		&cli.UintFlag{Name: "token-id", Usage: "token: the forged token ID, instead of --word (for guests with another tokenizer)"},
		&cli.Uint64Flag{Name: "checkpoint-interval", Usage: "Instructions between saved machine copies, at first (copies are thinned to at most 64)", Value: liar.DefaultCheckpointInterval},
		&cli.Uint64Flag{Name: "max-steps", Usage: "Give up if the honest run has not halted after this many instructions (0 = no limit)"},
		&cli.BoolFlag{Name: "json", Usage: "Print the full dispute as JSON"},
	),
}
