package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/urfave/cli/v2"
)

type capabilities struct {
	Profile  string            `json:"profile"`
	Formats  map[string]string `json:"formats"`
	Commands []string          `json:"commands"`
	Notes    []string          `json:"notes"`
}

func CapabilitiesAction(ctx *cli.Context) error {
	caps := capabilities{
		Profile: "mlvm-1",
		Formats: map[string]string{
			"manifest":        "mlvm.manifest/1",
			"snapshot":        "mlvm.snapshot/3",
			"stateWitness":    "mlvm.state-witness/1",
			"filesystemProof": "mlvm.filesystem-proof/2",
			"riscvStep":       "mlvm.riscv-step/1",
			"decoderResult":   "mlvm.decoder-result/1",
			"dispute":         "mlvm.dispute/0",
			"disputeRecord":   "mlvm.dispute-record/1",
		},
		Commands: []string{
			"load-elf", "run", "witness", "ml-claim",
			"demo", "xray", "liar", "referee", "capabilities",
			"float-subgame-fixture", "softfloat-vectors",
		},
		Notes: []string{
			"mlvm-1 never reinterprets frozen format IDs; new semantics require a new profile",
			"dispute/0 is experimental and excludes bonds, timeouts, and production deployment",
		},
	}
	if ctx.Bool("json") {
		enc := json.NewEncoder(ctx.App.Writer)
		enc.SetIndent("", "  ")
		return enc.Encode(caps)
	}
	fmt.Fprintf(ctx.App.Writer, "profile: %s\n", caps.Profile)
	for k, v := range caps.Formats {
		fmt.Fprintf(ctx.App.Writer, "  %-16s %s\n", k, v)
	}
	return nil
}

var CapabilitiesCommand = &cli.Command{
	Name:  "capabilities",
	Usage: "Print the mlvm-1 format and command surface",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "json", Usage: "Emit machine-readable JSON"},
	},
	Action: CapabilitiesAction,
}
