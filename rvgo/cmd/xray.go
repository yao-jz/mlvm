package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/internal/e2edemo"
	"github.com/yao-jz/mlvm/rvgo/internal/xray"
)

// tokenMarker records the step at which the decoder prints each generated token, so a
// profile can be split into "load and prompt" and "per generated token".
func tokenMarker(state *fast.VMState, profiler *xray.Profiler, echo io.Writer) io.Writer {
	watch := &e2edemo.TokenWatcher{
		Now:     func() uint64 { return state.Step },
		OnStart: func(at uint64) { profiler.Mark(at, "first token starts") },
		OnToken: func(text string, at uint64) { profiler.Mark(at, text) },
	}
	return io.MultiWriter(echo, watch)
}

func XrayAction(ctx *cli.Context) error {
	g, err := loadToolGuest(ctx)
	if err != nil {
		return err
	}
	state, label := g.State, g.Label
	profiler, err := xray.NewProfiler(g.ELF)
	if err != nil {
		return err
	}
	var guestOut io.Writer = io.Discard
	if ctx.Path("elf") != "" && !ctx.Bool("json") {
		guestOut = os.Stderr
	}
	marker := tokenMarker(state, profiler, guestOut)
	inst := fast.NewInstrumentedState(state, nil, marker, guestOut)
	maxSteps := ctx.Uint64("max-steps")
	for !state.Exited {
		if maxSteps > 0 && profiler.Steps() >= maxSteps {
			break
		}
		if err := profiler.Step(inst, state); err != nil {
			return fmt.Errorf("step %d: %w", state.Step, err)
		}
	}
	rep := profiler.Report()
	rep.Description = label

	if out := ctx.Path("svg"); out != "" {
		svg := rep.FlameSVG(xray.FlameOptions{
			Title:    "MLVM X-ray · " + label,
			Subtitle: fmt.Sprintf("%s RISC-V instructions, exact and deterministic (no sampling) · %.1f%% floating point", xrayGroup(rep.Steps), 100*rep.FloatShare),
		})
		if err := os.WriteFile(out, svg, 0o644); err != nil {
			return err
		}
	}
	if out := ctx.Path("folded"); out != "" {
		if err := os.WriteFile(out, []byte(rep.Folded()), 0o644); err != nil {
			return err
		}
	}
	if ctx.Bool("json") {
		rep.Tree = rep.Tree.Trim(rep.Steps, ctx.Float64("min-share"))
		enc := json.NewEncoder(ctx.App.Writer)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	printXray(ctx.App.Writer, rep, ctx.Int("top"))
	if out := ctx.Path("svg"); out != "" {
		fmt.Fprintf(ctx.App.Writer, "\n  flame graph  %s\n", out)
	}
	if out := ctx.Path("folded"); out != "" {
		fmt.Fprintf(ctx.App.Writer, "  folded       %s  (flamegraph.pl, inferno, speedscope)\n", out)
	}
	return nil
}

func xrayGroup(n uint64) string {
	s := fmt.Sprintf("%d", n)
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}

func bar(share float64, width int) string {
	n := int(share*float64(width) + 0.5)
	if n == 0 && share > 0 {
		return "▏"
	}
	return strings.Repeat("█", n)
}

func printXray(w io.Writer, rep *xray.Report, top int) {
	fmt.Fprintf(w, "MLVM X-ray  ·  %s\n\n", rep.Description)
	fmt.Fprintf(w, "  %s instructions · %.1f%% floating point · exact counts, identical on every machine\n\n", xrayGroup(rep.Steps), 100*rep.FloatShare)

	// Wide enough for the longest name shown (Rust paths run long), within reason.
	col := 24
	for i, f := range rep.Functions {
		if i >= top {
			break
		}
		col = max(col, min(44, len(xray.ShortName(f.Name))))
	}
	fmt.Fprintf(w, "  Where the instructions go (self)\n")
	for i, f := range rep.Functions {
		if i >= top {
			break
		}
		fmt.Fprintf(w, "    %-*s %5.1f%%  %-25s %14s\n", col, truncate(f.Name, col), 100*f.Share, bar(f.Share, 25), xrayGroup(f.Self))
	}

	fmt.Fprintf(w, "\n  Instruction classes\n")
	for _, c := range rep.Classes {
		fmt.Fprintf(w, "    %-*s %5.1f%%  %-25s %14s\n", col, c.Name, 100*c.Share, bar(c.Share, 25), xrayGroup(c.Count))
	}

	fmt.Fprintf(w, "\n  Hottest instructions\n    ")
	for i, m := range rep.Mnemonics {
		if i >= top {
			break
		}
		fmt.Fprintf(w, "%s %.1f%%   ", m.Name, 100*m.Share)
		if i%6 == 5 {
			fmt.Fprintf(w, "\n    ")
		}
	}
	fmt.Fprintln(w)

	if s, d := rep.Precision["s"], rep.Precision["d"]; s+d > 0 {
		fmt.Fprintf(w, "\n  Rounding float arithmetic: %s single precision, %s double precision\n", xrayGroup(s), xrayGroup(d))
	}

	// Token phases, when the guest printed tokens.
	var tokenSteps []uint64
	var first uint64
	for _, m := range rep.Marks {
		if m.Label == "first token starts" {
			first = m.Step
			continue
		}
		tokenSteps = append(tokenSteps, m.Step)
	}
	if len(tokenSteps) > 1 {
		perToken := (tokenSteps[len(tokenSteps)-1] - tokenSteps[0]) / uint64(len(tokenSteps)-1)
		fmt.Fprintf(w, "\n  Timeline: %s instructions to load the model and read the prompt, first token at %s,\n", xrayGroup(first), xrayGroup(tokenSteps[0]))
		fmt.Fprintf(w, "  then %s instructions per generated token (one forward pass each)\n", xrayGroup(perToken))
	}
}

func truncate(s string, n int) string { return xray.Fit(xray.ShortName(s), n) }

var XrayCommand = &cli.Command{
	Name:      "xray",
	Usage:     "Profile a guest instruction by instruction: functions, call stacks, instruction mix",
	ArgsUsage: "[prompt]",
	Description: "Runs the embedded TinyStories decoder (or --demo mlp, or any --elf) and attributes every " +
		"executed RISC-V instruction to its function and call stack. Counts are exact and deterministic. " +
		"--svg writes a flame graph colored by floating-point share; --folded writes stacks for flamegraph.pl, inferno or speedscope.",
	Action: XrayAction,
	Flags: append(guestFlags("Profile"),
		&cli.Uint64Flag{Name: "max-steps", Usage: "Stop after this many instructions (0 = run to exit)"},
		&cli.PathFlag{Name: "svg", Usage: "Write a flame graph SVG to this path"},
		&cli.PathFlag{Name: "folded", Usage: "Write folded call stacks to this path"},
		&cli.BoolFlag{Name: "json", Usage: "Print the full report as JSON"},
		&cli.Float64Flag{Name: "min-share", Usage: "With --json, fold call-tree frames below this share into their parent", Value: 0.0005},
		&cli.IntFlag{Name: "top", Usage: "Rows per table", Value: 12},
	),
}
