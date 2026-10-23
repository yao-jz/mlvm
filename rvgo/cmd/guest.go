package cmd

import (
	"bytes"
	"debug/elf"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/internal/e2edemo"
)

// toolGuest is the program `mlvm xray` and `mlvm liar` run: an embedded demo, or an ELF
// from --elf with its --load-file inputs staged.
type toolGuest struct {
	State  *fast.VMState
	ELF    []byte
	Label  string
	Demo   string // "decoder" or "mlp" for an embedded demo, "" for --elf
	Prompt string // decoder only
	// TinyStories: the guest commits TinyStories token IDs at MLVM_OUT, in the decoder's
	// layout (docs/DECODER-PROTOCOL.md). True for the decoder, and for an --elf guest that
	// stages the same tokenizer as tokenizer.bin.
	TinyStories bool
}

// guestFlags select the guest, shared by the tools that take one.
func guestFlags(verb string) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "demo", Usage: "Embedded guest: decoder (default) or mlp"},
		&cli.IntFlag{Name: "max-new-tokens", Usage: "Decoder generation length (1..16)", Value: e2edemo.DefaultMaxNewTokens},
		&cli.PathFlag{Name: "elf", Usage: verb + " this RV64IMAFD ELF instead of an embedded demo"},
		LoadFileFlag,
		RandomFileFlag,
		&cli.StringFlag{Name: "label", Usage: "Name the guest in the output (default: the demo, or the ELF path)"},
	}
}

func loadToolGuest(ctx *cli.Context) (*toolGuest, error) {
	if path := ctx.Path("elf"); path != "" {
		if ctx.String("demo") != "" {
			return nil, fmt.Errorf("--elf and --demo select different guests; pass one")
		}
		elfBytes, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		f, err := elf.NewFile(bytes.NewReader(elfBytes))
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		defer f.Close()
		state, err := fast.LoadELF(f)
		if err != nil {
			return nil, err
		}
		if err := fast.PatchVM(f, state); err != nil {
			return nil, err
		}
		if err := state.LoadRandomNumberFile(ctx.Path(RandomFileFlag.Name)); err != nil {
			return nil, err
		}
		if err := fast.LoadFile(ctx.StringSlice(LoadFileFlag.Name), state); err != nil {
			return nil, err
		}
		label := ctx.String("label")
		if label == "" {
			label = path
		}
		g := &toolGuest{State: state, ELF: elfBytes, Label: label}
		for _, spec := range ctx.StringSlice(LoadFileFlag.Name) {
			name, host, ok := strings.Cut(spec, "=")
			if !ok {
				host = name
			}
			if name == "tokenizer.bin" {
				if data, err := os.ReadFile(host); err == nil && e2edemo.IsTinyStoriesTokenizer(data) {
					g.TinyStories = true
				}
			}
		}
		return g, nil
	}
	if len(ctx.StringSlice(LoadFileFlag.Name)) > 0 {
		return nil, fmt.Errorf("--load-file stages inputs for --elf; the embedded demos bring their own")
	}
	g, err := e2edemo.NewGuest(ctx.String("demo"), PromptArg(ctx), ctx.Int("max-new-tokens"))
	if err != nil {
		return nil, err
	}
	label := fmt.Sprintf("TinyStories-260K decoder, %q, %d tokens", g.Prompt, g.MaxNewTokens)
	if g.Demo == "mlp" {
		label = "embedded MLP"
	}
	if l := ctx.String("label"); l != "" {
		label = l
	}
	return &toolGuest{State: g.State, ELF: g.ELF, Label: label, Demo: g.Demo, Prompt: g.Prompt, TinyStories: g.Demo == "decoder"}, nil
}
