package cmd

import (
	"debug/elf"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfave/cli/v2"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

// MLClaim derives the claim a guest's run supports: the machine it started from, the machine
// it ended at, and the answer it committed.
//
// The prestate is the interesting half. It is a pure function of the program, the files
// staged into memory and the randomness blob, so anyone holding the same model and input can
// recompute it and check that a published claim is about what they think it is. Nothing about
// it is secret and nothing about it depends on the run.
//
// Run:  mlvm ml-claim --path examples/build/mlp
//       mlvm ml-claim --path <elf> --load-file model.safetensors --prestate

var MLClaimPathFlag = &cli.PathFlag{
	Name:     "path",
	Usage:    "Path to the guest ELF",
	Required: true,
}

var MLClaimLoadFileFlag = &cli.StringSliceFlag{
	Name:  "load-file",
	Usage: "File to stage into guest memory; repeat for several. Part of the prestate.",
}

var MLClaimRandomFileFlag = &cli.PathFlag{
	Name:  "random-file",
	Usage: "Optional deterministic byte blob (up to 5 MiB) for getrandom(2). Part of the prestate.",
}

var MLClaimPrestateOnlyFlag = &cli.BoolFlag{
	Name:  "prestate",
	Usage: "Print the prestate hash and stop, without running the guest",
}

var MLClaimOutFlag = &cli.PathFlag{
	Name:  "out",
	Usage: "Where to write the JSON claim (stdout if empty)",
}

var MLClaimFixtureFlag = &cli.PathFlag{
	Name:  "fixture",
	Usage: "Also write a Solidity test fixture, with the final witness and its merkle branch",
}

var MLClaimNoResultFlag = &cli.PathFlag{
	Name: "fixture-no-result",
	Usage: "A second guest, one that never commits a result, to include in the fixture as the " +
		"negative case. Without a real run of one, the contract's magic check can only be " +
		"tested by corrupting a proof - which exercises the branch check instead.",
}

func MLClaimAction(ctx *cli.Context) error {
	path := ctx.Path(MLClaimPathFlag.Name)
	f, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open guest ELF %q: %w", path, err)
	}
	defer f.Close()

	state, err := fast.LoadELF(f)
	if err != nil {
		return fmt.Errorf("failed to load guest: %w", err)
	}
	if err := fast.PatchVM(f, state); err != nil {
		return fmt.Errorf("failed to patch guest: %w", err)
	}
	if err := state.LoadRandomNumberFile(ctx.Path(MLClaimRandomFileFlag.Name)); err != nil {
		return err
	}
	if err := fast.LoadFile(ctx.StringSlice(MLClaimLoadFileFlag.Name), state); err != nil {
		return fmt.Errorf("failed to stage input files: %w", err)
	}

	if ctx.Bool(MLClaimPrestateOnlyFlag.Name) {
		pre, err := state.EncodeWitness().StateHash()
		if err != nil {
			return err
		}
		fmt.Printf("%x\n", pre)
		return nil
	}

	claim, err := fast.MakeMLClaim(state, 0)
	if err != nil {
		return err
	}

	out, err := json.MarshalIndent(claim, "", "  ")
	if err != nil {
		return err
	}
	if dest := ctx.Path(MLClaimOutFlag.Name); dest != "" {
		if err := os.WriteFile(dest, out, 0o644); err != nil {
			return err
		}
	} else {
		fmt.Println(string(out))
	}

	if dest := ctx.Path(MLClaimFixtureFlag.Name); dest != "" {
		proof := fast.MLResultProof(state)
		fixture := struct {
			PreState  string `json:"preState"`
			PostState string `json:"postState"`
			Steps     uint64 `json:"steps"`
			Answer    string `json:"answer"`
			// Foundry's JSON parser guesses a value's type from its shape, and it guesses
			// wrong for a 40-byte hex string - it hands back the ASCII of the string rather
			// than the bytes. The hash is a bytes32, which it never mis-guesses, and it is
			// what a dispute game would compare anyway.
			AnswerHash string `json:"answerHash"`
			StateData  string `json:"stateData"`
			Proof      string `json:"proof"`
			// A guest that halts without committing anything, so the contract's "no result"
			// path is tested against a real state rather than a doctored one.
			NoResultState string `json:"noResultState"`
			NoResultProof string `json:"noResultProof"`
		}{
			PreState:   fmt.Sprintf("0x%x", claim.PreState),
			PostState:  fmt.Sprintf("0x%x", claim.PostState),
			Steps:      claim.Steps,
			Answer:     fmt.Sprintf("0x%x", claim.Answer),
			AnswerHash: fmt.Sprintf("0x%x", crypto.Keccak256(claim.Answer)),
			StateData:  fmt.Sprintf("0x%x", []byte(state.EncodeWitness())),
			Proof:      fmt.Sprintf("0x%x", proof),
		}
		if other := ctx.Path(MLClaimNoResultFlag.Name); other != "" {
			st, err := runToHalt(other, ctx.Path(MLClaimRandomFileFlag.Name))
			if err != nil {
				return fmt.Errorf("no-result guest %q: %w", other, err)
			}
			if _, ok := fast.MLResult(st); ok {
				return fmt.Errorf("guest %q does commit a result; it cannot serve as the negative case", other)
			}
			p := fast.MLResultProof(st)
			fixture.NoResultState = fmt.Sprintf("0x%x", []byte(st.EncodeWitness()))
			fixture.NoResultProof = fmt.Sprintf("0x%x", p)
		}

		blob, err := json.MarshalIndent(&fixture, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(dest, blob, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote fixture to %s\n", dest)
	}
	return nil
}

var MLClaimCommand = &cli.Command{
	Name:        "ml-claim",
	Usage:       "Derive the inference claim a guest run supports",
	Description: "Prints the prestate, the poststate and the answer the guest committed to memory.",
	Action:      MLClaimAction,
	Flags: []cli.Flag{
		MLClaimPathFlag,
		MLClaimLoadFileFlag,
		MLClaimRandomFileFlag,
		MLClaimPrestateOnlyFlag,
		MLClaimOutFlag,
		MLClaimFixtureFlag,
		MLClaimNoResultFlag,
	},
}

// runToHalt loads a guest and runs it to completion, without requiring it to commit a result.
func runToHalt(path, randomFile string) (*fast.VMState, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	state, err := fast.LoadELF(f)
	if err != nil {
		return nil, err
	}
	if err := fast.PatchVM(f, state); err != nil {
		return nil, err
	}
	if err := state.LoadRandomNumberFile(randomFile); err != nil {
		return nil, err
	}
	in := fast.NewInstrumentedState(state, nil, os.Stderr, os.Stderr)
	for !state.Exited {
		if state.Step > 50_000_000 {
			return nil, fmt.Errorf("did not halt within 50M steps")
		}
		if _, err := in.Step(false); err != nil {
			return nil, err
		}
	}
	return state, nil
}
