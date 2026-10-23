package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/yao-jz/mlvm/rvgo/internal/liar"
)

// disputeRecord is what `mlvm referee` reads from a dispute record: the fields of
// LiarReport it shows, and the dispute itself.
type disputeRecord struct {
	Format string `json:"format"`
	Guest  string `json:"guest"`
	Lie    string `json:"lie"`
	liar.Result
}

func RefereeAction(ctx *cli.Context) error {
	if ctx.NArg() != 1 {
		return fmt.Errorf("usage: mlvm referee DISPUTE.json (a record from `mlvm liar --json` or the playground)")
	}
	data, err := os.ReadFile(ctx.Args().First())
	if err != nil {
		return err
	}
	var rec disputeRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return fmt.Errorf("%s: %w", ctx.Args().First(), err)
	}
	if rec.Format != liar.RecordFormat {
		return fmt.Errorf("%s: format %q, want %q", ctx.Args().First(), rec.Format, liar.RecordFormat)
	}
	v, err := liar.Recheck(&rec.Result)
	if err != nil {
		return fmt.Errorf("the record does not hold up: %w", err)
	}
	if v.EVM != nil && !v.EVM.Matches {
		return fmt.Errorf("RISCV.sol in the EVM computes %s, slow.Step %s", v.EVM.Post.Hex(), v.RefereePost.Hex())
	}
	// The record's own verdict must be the one the referee reaches now.
	if rv := rec.Verdict; rv.RefereePost != v.RefereePost || rv.Winner != v.Winner {
		return fmt.Errorf("the record says the referee computed %s (%s wins), but it computes %s (%s wins)",
			rv.RefereePost.Hex(), rv.Winner, v.RefereePost.Hex(), v.Winner)
	}

	w := ctx.App.Writer
	if ctx.Bool("json") {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	total := max(rec.HonestSteps, rec.LiarSteps)
	fmt.Fprintf(w, "MLVM  ·  Referee\n\n")
	fmt.Fprintf(w, "  A record of a finished dispute, checked again without running either machine.\n\n")
	if rec.Guest != "" {
		fmt.Fprintf(w, "  program  %s\n", rec.Guest)
	}
	if rec.Lie != "" {
		fmt.Fprintf(w, "  lie      %s (the liar's own account; not needed for the ruling)\n", rec.Lie)
	}
	fmt.Fprintf(w, "  both start from prestate %s\n", rec.Prestate.Hex())
	fmt.Fprintf(w, "  honest   %15s instructions, poststate %s\n", xrayGroup(rec.HonestSteps), rec.HonestPost.Hex())
	fmt.Fprintf(w, "  liar     %15s instructions, poststate %s\n", xrayGroup(rec.LiarSteps), rec.LiarPost.Hex())
	fmt.Fprintf(w, "\n  ✓ %d questions, each about the midpoint of the instructions still in dispute, narrow\n", len(rec.Rounds))
	fmt.Fprintf(w, "    %s instructions to step %s.\n", xrayGroup(total), xrayGroup(v.Step))
	fmt.Fprintf(w, "  ✓ The %d-byte state witness hashes to the state both machines agreed on before it.\n", (len(v.StateWitness)-2)/2)
	fmt.Fprintf(w, "  ✓ The instruction word is proven by a Merkle proof against the witness's memory root.\n")
	fmt.Fprintf(w, "  ✓ The record's verdict is the one the referee reaches now.\n")
	printVerdict(w, v)
	return nil
}

var RefereeCommand = &cli.Command{
	Name:      "referee",
	Usage:     "Check a recorded dispute again: the bisection, the witness, and the one disputed instruction",
	ArgsUsage: "DISPUTE.json",
	Description: "Reads the record of a finished dispute, as written by `mlvm liar --json` or downloaded from " +
		"the playground (format mlvm.dispute-record/1), and rules on it the way an independent referee " +
		"would: it runs neither machine. It checks that the questions form a bisection ending on one " +
		"instruction, that the state witness hashes to the state both parties agreed on before it, and then " +
		"executes that instruction from the witness and its Merkle proofs: with RISCV.sol's compiled step " +
		"function in an in-process EVM and slow.Step for integer and memory instructions, with SoftFloat for " +
		"floating-point arithmetic. It fails unless the ruling matches the record's.",
	Action: RefereeAction,
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "json", Usage: "Print the verdict as JSON"},
	},
}
