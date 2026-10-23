//go:build js && wasm

// Command wasm is MLVM in a browser tab: the same fast VM, profiler and dispute parties
// as the CLI, compiled to WebAssembly and exposed to JavaScript as globalThis.mlvm.
//
// Each Web Worker loads its own instance and plays one role: an honest machine, a lying
// machine, or the referee. Functions take and return JSON strings; long runs report
// progress through a callback, which posts messages while the run continues.
// Build with `make web`; the page lives in web/.
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"syscall/js"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/internal/e2edemo"
	"github.com/yao-jz/mlvm/rvgo/internal/liar"
	"github.com/yao-jz/mlvm/rvgo/internal/rvdis"
	"github.com/yao-jz/mlvm/rvgo/internal/xray"
)

var (
	party *liar.Party
	sym   *xray.Symbolizer
	vocab *e2edemo.Vocab
)

type faultConfig struct {
	Kind     string `json:"kind"`
	AtStep   uint64 `json:"atStep"`
	Mnemonic string `json:"mnemonic"`
	Function string `json:"function"`
	// TokenIndex and Word: the token lie. OutputOffset and Output: the output lie prints
	// Output in place of the bytes at OutputOffset of stdout.
	TokenIndex   int    `json:"tokenIndex"`
	Word         string `json:"word"`
	OutputOffset uint64 `json:"outputOffset"`
	Output       string `json:"output"`
}

type runConfig struct {
	Role         string       `json:"role"`
	Prompt       string       `json:"prompt"`
	MaxNewTokens int          `json:"maxNewTokens"`
	Interval     uint64       `json:"interval"`
	TickEvery    uint64       `json:"tickEvery"`
	Profile      bool         `json:"profile"`
	Fault        *faultConfig `json:"fault"`
	// HonestPoststate lets a lying machine retry when its kept lie vanishes before the end
	// (see liar.RunUntilDisputed), and HonestSteps when an integer lie derails the program.
	HonestPoststate string `json:"honestPoststate"`
	HonestSteps     uint64 `json:"honestSteps"`
}

type attempt struct {
	liar.Attempt
	Function string `json:"function"`
}

type runSummary struct {
	Role       string          `json:"role"`
	Prompt     string          `json:"prompt"`
	Steps      uint64          `json:"steps"`
	Prestate   string          `json:"prestate"`
	Poststate  string          `json:"poststate"`
	Tokens     []uint16        `json:"tokens"`
	Text       string          `json:"text"`
	Stdout     string          `json:"stdout"`
	Lie        string          `json:"lie,omitempty"`
	Pages      int             `json:"pages"`
	Millis     int64           `json:"millis"`
	Deviation  *liar.Deviation `json:"deviation,omitempty"`
	DevFunc    string          `json:"deviationFunction,omitempty"`
	Attempts   []attempt       `json:"attempts,omitempty"`
	Xray       *xray.Report    `json:"xray,omitempty"`
	Checkpoint uint64          `json:"checkpointInterval"`
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(b)
}

func fail(err error) any { return toJSON(map[string]string{"error": err.Error()}) }

func funcName(pc uint64) string {
	if sym == nil {
		return ""
	}
	return sym.Name(sym.Lookup(pc))
}

func ensureVocab() error {
	if vocab != nil {
		return nil
	}
	v, err := e2edemo.LoadVocab()
	vocab = v
	return err
}

// partyRun(configJSON, onEvent) runs one machine to completion and returns its summary.
func partyRun(_ js.Value, args []js.Value) any {
	var cfg runConfig
	if err := json.Unmarshal([]byte(args[0].String()), &cfg); err != nil {
		return fail(err)
	}
	cb := args[1]
	emit := func(v any) { cb.Invoke(toJSON(v)) }
	if err := ensureVocab(); err != nil {
		return fail(err)
	}
	g, err := e2edemo.NewDecoderGuest(cfg.Prompt, cfg.MaxNewTokens)
	if err != nil {
		return fail(err)
	}
	if sym, err = xray.NewSymbolizer(g.ELF); err != nil {
		return fail(err)
	}
	pre, err := g.State.EncodeWitness().StateHash()
	if err != nil {
		return fail(err)
	}
	emit(map[string]any{"type": "prestate", "prestate": pre.Hex()})

	var fault *liar.Fault
	if cfg.Fault != nil {
		fault = &liar.Fault{Kind: liar.FaultKind(cfg.Fault.Kind), AtStep: cfg.Fault.AtStep, Mnemonic: cfg.Fault.Mnemonic, TokenIndex: cfg.Fault.TokenIndex}
		if cfg.HonestSteps > 0 {
			// A lie can derail the program; past this, the liar treats the run as crashed.
			fault.StepLimit = 2*cfg.HonestSteps + 1<<20
		}
		switch fault.Kind {
		case liar.FaultULP, liar.FaultLoad, liar.FaultInt:
			if fn := cfg.Fault.Function; fn != "" {
				if fault.InFunction, err = inFunction(fn); err != nil {
					return fail(err)
				}
				fault.Function = fn
			}
		case liar.FaultOutput:
			if cfg.Fault.Output == "" {
				return fail(fmt.Errorf("the output lie needs the bytes to print"))
			}
			fault.OutputOffset, fault.Output = cfg.Fault.OutputOffset, []byte(cfg.Fault.Output)
		case liar.FaultToken:
			id, ok := vocab.Lookup(" " + strings.TrimSpace(cfg.Fault.Word))
			if !ok {
				if id, ok = vocab.Lookup(cfg.Fault.Word); !ok {
					return fail(fmt.Errorf("%q is not a single token of the TinyStories vocabulary", cfg.Fault.Word))
				}
			}
			fault.Token = id
		default:
			return fail(fmt.Errorf("unknown lie %q: want ulp, load, int, output or token", cfg.Fault.Kind))
		}
	}
	var profiler *xray.Profiler
	if cfg.Profile {
		if profiler, err = xray.NewProfiler(g.ELF); err != nil {
			return fail(err)
		}
	}
	tick := cfg.TickEvery
	if tick == 0 {
		tick = 1 << 18
	}
	runs := 0
	var stdout *bytes.Buffer // this run's, so a restarted liar starts it over
	setup := func(p *liar.Party) {
		runs++
		stdout = new(bytes.Buffer)
		if runs > 1 {
			emit(map[string]any{"type": "restart", "run": runs})
		}
		var lastTick uint64
		watch := &e2edemo.TokenWatcher{
			Now:     func() uint64 { return lastTick },
			OnToken: func(text string, at uint64) { emit(map[string]any{"type": "token", "text": text, "step": at}) },
		}
		if profiler != nil {
			p.SetStepper(profiler.Step)
			watch.Now = profiler.Steps
			watch.OnStart = func(at uint64) { profiler.Mark(at, "first token starts") }
			watch.OnToken = func(text string, at uint64) {
				profiler.Mark(at, text)
				emit(map[string]any{"type": "token", "text": text, "step": at})
			}
		}
		p.SetStdout(io.MultiWriter(watch, stdout))
		p.SetTick(tick, func(step uint64) {
			lastTick = step
			emit(map[string]any{"type": "tick", "step": step})
		})
	}

	start := time.Now()
	if fault != nil && cfg.HonestPoststate != "" {
		party, err = liar.RunUntilDisputed(cfg.Role, g.State, *fault, cfg.Interval, common.HexToHash(cfg.HonestPoststate), 5, setup)
	} else {
		party = liar.NewParty(cfg.Role, g.State, fault, cfg.Interval)
		setup(party)
		err = party.Run(0, nil)
	}
	if err != nil {
		party = nil
		return fail(err)
	}
	final := party.Final()
	tokens, err := e2edemo.ResultTokens(final)
	if err != nil {
		return fail(err)
	}
	out := runSummary{
		Role:       cfg.Role,
		Prompt:     g.Prompt,
		Steps:      party.Steps(),
		Prestate:   pre.Hex(),
		Poststate:  party.FinalHash().Hex(),
		Tokens:     tokens,
		Text:       vocab.Text(tokens),
		Stdout:     stdout.String(),
		Pages:      final.Memory.PageCount(),
		Millis:     time.Since(start).Milliseconds(),
		Deviation:  party.Deviation(),
		Checkpoint: cfg.Interval,
	}
	if fault != nil {
		out.Lie = fault.String()
	}
	if out.Deviation != nil {
		out.DevFunc = funcName(out.Deviation.PC)
	}
	for _, a := range party.Attempts() {
		out.Attempts = append(out.Attempts, attempt{Attempt: a, Function: funcName(a.PC)})
	}
	if profiler != nil {
		rep := profiler.Report()
		rep.Tree = rep.Tree.Trim(rep.Steps, 0.001)
		if len(rep.Functions) > 40 {
			rep.Functions = rep.Functions[:40]
		}
		if len(rep.Mnemonics) > 24 {
			rep.Mnemonics = rep.Mnemonics[:24]
		}
		out.Xray = rep
	}
	return toJSON(out)
}

// inFunction accepts the program counters of the decoder function called name.
func inFunction(name string) (func(pc uint64) bool, error) {
	match := map[int]bool{}
	for i := 0; i < sym.Len(); i++ {
		if sym.Name(i) == name {
			match[i] = true
		}
	}
	if len(match) == 0 {
		return nil, fmt.Errorf("no function %q in the decoder", name)
	}
	return func(pc uint64) bool { return match[sym.Lookup(pc)] }, nil
}

// hashAt(step) answers a bisection question.
func hashAt(_ js.Value, args []js.Value) any {
	if party == nil {
		return fail(fmt.Errorf("no machine has run in this worker"))
	}
	h, err := party.HashAt(uint64(args[0].Float()))
	if err != nil {
		return fail(err)
	}
	return toJSON(map[string]string{"hash": h.Hex()})
}

// observe(roundJSON) tells this machine how a round ended.
func observe(_ js.Value, args []js.Value) any {
	if party == nil {
		return fail(fmt.Errorf("no machine has run in this worker"))
	}
	var r liar.Round
	if err := json.Unmarshal([]byte(args[0].String()), &r); err != nil {
		return fail(err)
	}
	party.Observe(r)
	return toJSON(map[string]bool{"ok": true})
}

type transitionJSON struct {
	Step     uint64 `json:"step"`
	PC       uint64 `json:"pc"`
	Word     uint32 `json:"word"`
	Pre      string `json:"pre"`
	Post     string `json:"post"`
	State    string `json:"state"`
	MemProof string `json:"memProof"`
	FSProof  string `json:"fsProof"`
}

// transitionAt(step) defends one instruction: witness, proofs and claimed post-state.
func transitionAt(_ js.Value, args []js.Value) any {
	if party == nil {
		return fail(fmt.Errorf("no machine has run in this worker"))
	}
	t, err := party.TransitionAt(uint64(args[0].Float()))
	if err != nil {
		return fail(err)
	}
	return toJSON(transitionJSON{
		Step: t.Step, PC: t.PC, Word: t.Word, Pre: t.Pre.Hex(), Post: t.Post.Hex(),
		State:    hex.EncodeToString(t.Witness.State),
		MemProof: hex.EncodeToString(t.Witness.MemProof),
		FSProof:  hex.EncodeToString(t.Witness.FileSystemProof),
	})
}

// referee(transitionJSON, liarPostHex) rules on a defended instruction. It uses only the
// witness and proofs, never a party's machine.
func referee(_ js.Value, args []js.Value) any {
	var tj transitionJSON
	if err := json.Unmarshal([]byte(args[0].String()), &tj); err != nil {
		return fail(err)
	}
	dec := func(s string) []byte { b, _ := hex.DecodeString(s); return b }
	t := &liar.Transition{
		Step: tj.Step, PC: tj.PC, Word: tj.Word,
		Pre: common.HexToHash(tj.Pre), Post: common.HexToHash(tj.Post),
		Witness: &fast.StepWitness{State: dec(tj.State), MemProof: dec(tj.MemProof), FileSystemProof: dec(tj.FSProof)},
	}
	v, err := liar.Referee(t)
	if err != nil {
		return fail(err)
	}
	v.HonestPost = t.Post
	v.LiarPost = common.HexToHash(args[1].String())
	if sym == nil && len(args) > 2 {
		// The referee worker never ran a machine; it symbolizes with the demo ELF.
		if g, err := e2edemo.NewDecoderGuest("", 1); err == nil {
			sym, _ = xray.NewSymbolizer(g.ELF)
		}
	}
	v.Function = funcName(v.PC)
	switch {
	case v.HonestPost == v.LiarPost:
		v.Winner = "none: both machines claim the same post-state"
	case v.RefereePost == v.HonestPost:
		v.Winner = "honest"
	case v.RefereePost == v.LiarPost:
		v.Winner = "liar"
	default:
		v.Winner = "neither"
	}
	return toJSON(v)
}

// recheck(recordJSON) rules on the record of a finished dispute (mlvm.dispute-record/1)
// from the record alone, as `mlvm referee` does, without the EVM.
func recheck(_ js.Value, args []js.Value) any {
	var rec struct {
		Format string `json:"format"`
		liar.Result
	}
	if err := json.Unmarshal([]byte(args[0].String()), &rec); err != nil {
		return fail(err)
	}
	if rec.Format != liar.RecordFormat {
		return fail(fmt.Errorf("format %q, want %q", rec.Format, liar.RecordFormat))
	}
	start := time.Now()
	v, err := liar.Recheck(&rec.Result)
	if err != nil {
		return fail(err)
	}
	return toJSON(map[string]any{
		"verdict":       v,
		"micros":        time.Since(start).Microseconds(),
		"matchesRecord": rec.Verdict.RefereePost == v.RefereePost && rec.Verdict.Winner == v.Winner,
	})
}

// disasm(word, pc) returns objdump-style text for one instruction.
func disasm(_ js.Value, args []js.Value) any {
	inst := rvdis.Decode(uint32(args[0].Int()), uint64(args[1].Float()))
	return toJSON(map[string]string{"text": inst.Text(), "class": string(inst.Class)})
}

func main() {
	api := js.Global().Get("Object").New()
	api.Set("partyRun", js.FuncOf(partyRun))
	api.Set("hashAt", js.FuncOf(hashAt))
	api.Set("observe", js.FuncOf(observe))
	api.Set("transitionAt", js.FuncOf(transitionAt))
	api.Set("referee", js.FuncOf(referee))
	api.Set("recheck", js.FuncOf(recheck))
	api.Set("disasm", js.FuncOf(disasm))
	api.Set("version", "mlvm-1")
	js.Global().Set("mlvm", api)
	if ready := js.Global().Get("mlvmReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {}
}
