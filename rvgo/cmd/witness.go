package cmd

import (
	"fmt"
	"os"

	cannon "github.com/ethereum-optimism/optimism/cannon/cmd"
	"github.com/ethereum-optimism/optimism/op-service/jsonutil"
	"github.com/urfave/cli/v2"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

func Witness(ctx *cli.Context) error {
	input := ctx.Path(cannon.WitnessInputFlag.Name)
	output := ctx.Path(cannon.WitnessOutputFlag.Name)
	state, err := jsonutil.LoadJSON[fast.VMState](input)
	if err != nil {
		return fmt.Errorf("invalid input state (%v): %w", input, err)
	}
	if err := state.ValidateSnapshot(); err != nil {
		return fmt.Errorf("invalid input state (%v): %w", input, err)
	}
	witness := state.EncodeWitness()
	h, err := witness.StateHash()
	if err != nil {
		return fmt.Errorf("failed to compute witness hash: %w", err)
	}
	if output != "" {
		if err := os.WriteFile(output, witness, 0o644); err != nil {
			return fmt.Errorf("writing output to %v: %w", output, err)
		}
	}
	fmt.Println(h.Hex())
	return nil
}

var WitnessCommand = &cli.Command{
	Name:        "witness",
	Usage:       "Convert an MLVM JSON state into a binary witness",
	Description: "Convert an MLVM JSON state into a binary witness and write its state hash to stdout.",
	Action:      Witness,
	Flags: []cli.Flag{
		cannon.WitnessInputFlag,
		cannon.WitnessOutputFlag,
	},
}
