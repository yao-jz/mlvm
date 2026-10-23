package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli/v2"

	"github.com/pkg/profile"

	cannon "github.com/ethereum-optimism/optimism/cannon/cmd"
	preimage "github.com/ethereum-optimism/optimism/op-preimage"
	"github.com/ethereum-optimism/optimism/op-service/jsonutil"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

type Proof struct {
	Step uint64 `json:"step"`

	Pre  common.Hash `json:"pre"`  // 32 bytes
	Post common.Hash `json:"post"` // 32 bytes

	StateData hexutil.Bytes `json:"state-data"`
	ProofData hexutil.Bytes `json:"proof-data"`

	OracleKey    hexutil.Bytes `json:"oracle-key,omitempty"`
	OracleValue  hexutil.Bytes `json:"oracle-value,omitempty"`
	OracleOffset uint64        `json:"oracle-offset,omitempty"`
}

type StepFn func(proof bool) (*fast.StepWitness, error)

func Guard(proc *os.ProcessState, fn StepFn) StepFn {
	return func(proof bool) (*fast.StepWitness, error) {
		wit, err := fn(proof)
		if err != nil {
			if proc.Exited() {
				return nil, fmt.Errorf("pre-image server exited with code %d, resulting in err %w", proc.ExitCode(), err)
			} else {
				return nil, err
			}
		}
		return wit, nil
	}
}

var _ fast.PreimageOracle = (*ProcessPreimageOracle)(nil)

var OutFilePerm = os.FileMode(0o644)

var LoadRuntimeFileFlag = &cli.StringSliceFlag{
	Name:     "load-file",
	Usage:    "Confirm a staged file is present in the input state; repeat for several",
	Required: false,
}

// RawOutputFlag pipes the guest's stdout/stderr straight through instead of wrapping each
// write in a logfmt line. Useful for demos and for diffing guest output against a native
// build of the same program (see scripts/determinism.sh).
var RawOutputFlag = &cli.BoolFlag{
	Name:     "raw-output",
	Usage:    "Write the guest program's stdout/stderr directly to the host's, unwrapped",
	Required: false,
}

// TraceFlag turns on the VM's per-step diagnostics: every syscall with its arguments,
// mmap/mremap decisions and file-descriptor bookkeeping, on stderr. Off by default
// because a program of any size issues tens of thousands of them.
var TraceFlag = &cli.BoolFlag{
	Name:     "trace",
	Usage:    "Log every syscall and memory-mapping decision to stderr",
	Required: false,
}

func Run(ctx *cli.Context) error {
	if ctx.Bool(cannon.RunPProfCPU.Name) { // cpu profiling
		defer profile.Start(profile.NoShutdownHook, profile.ProfilePath("."), profile.CPUProfile).Stop()
	}

	state, err := jsonutil.LoadJSON[fast.VMState](ctx.Path(cannon.RunInputFlag.Name))
	if err != nil {
		return err
	}
	if err := state.ValidateSnapshot(); err != nil {
		return fmt.Errorf("invalid input state %q: %w", ctx.Path(cannon.RunInputFlag.Name), err)
	}

	fast.Trace = ctx.Bool(TraceFlag.Name)

	l := Logger(os.Stderr, log.LevelInfo)
	var outLog, errLog io.Writer = &LoggingWriter{Name: "program std-out", Log: l},
		&LoggingWriter{Name: "program std-err", Log: l}
	if ctx.Bool(RawOutputFlag.Name) {
		outLog, errLog = os.Stdout, os.Stderr
	}

	// test randomnumber
	// fmt.Printf("random number file addr: %v, random number file offset: %v\n", state.RandomFileAddr, state.RandomFileOffset)
	// io.Copy(outLog, state.Memory.ReadMemoryRange(state.RandomFileAddr, 8))

	stopAtAnyPreimage := false
	var stopAtPreimageTypeByte preimage.KeyType
	switch ctx.String(cannon.RunStopAtPreimageTypeFlag.Name) {
	case "local":
		stopAtPreimageTypeByte = preimage.LocalKeyType
	case "keccak":
		stopAtPreimageTypeByte = preimage.Keccak256KeyType
	case "sha256":
		stopAtPreimageTypeByte = preimage.Sha256KeyType
	case "blob":
		stopAtPreimageTypeByte = preimage.BlobKeyType
	case "any":
		stopAtAnyPreimage = true
	case "":
		// 0 preimage type is forbidden so will not stop at any preimage
	default:
		return fmt.Errorf("invalid preimage type %q", ctx.String(cannon.RunStopAtPreimageTypeFlag.Name))
	}
	stopAtPreimageLargerThan := ctx.Int(cannon.RunStopAtPreimageLargerThanFlag.Name)

	// split CLI args after first '--'
	args := ctx.Args().Slice()
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		args = []string{""}
	}

	po, err := NewProcessPreimageOracle(args[0], args[1:])
	if err != nil {
		return fmt.Errorf("failed to create pre-image oracle process: %w", err)
	}
	if err := po.Start(); err != nil {
		return fmt.Errorf("failed to start pre-image oracle server: %w", err)
	}
	defer func() {
		if err := po.Close(); err != nil {
			l.Error("failed to close pre-image server", "err", err)
		}
	}()

	stopAt := ctx.Generic(cannon.RunStopAtFlag.Name).(*cannon.StepMatcherFlag).Matcher()
	proofAt := ctx.Generic(cannon.RunProofAtFlag.Name).(*cannon.StepMatcherFlag).Matcher()
	snapshotAt := ctx.Generic(cannon.RunSnapshotAtFlag.Name).(*cannon.StepMatcherFlag).Matcher()
	infoAt := ctx.Generic(cannon.RunInfoAtFlag.Name).(*cannon.StepMatcherFlag).Matcher()

	loadRuntimeFiles := ctx.StringSlice(LoadRuntimeFileFlag.Name)
	for _, name := range loadRuntimeFiles {
		if !state.FileSystem.Has(name) {
			return fmt.Errorf("file %q is not present in the input state's committed manifest; stage it with load-elf --load-file", name)
		}
	}

	var meta *Metadata
	if metaPath := ctx.Path(cannon.RunMetaFlag.Name); metaPath == "" {
		l.Info("no metadata file specified, defaulting to empty metadata")
		meta = &Metadata{Symbols: nil} // provide empty metadata by default
	} else {
		if m, err := jsonutil.LoadJSON[Metadata](metaPath); err != nil {
			return fmt.Errorf("failed to load metadata: %w", err)
		} else {
			meta = m
		}
	}

	us := fast.NewInstrumentedState(state, po, outLog, errLog)
	proofFmt := ctx.String(cannon.RunProofFmtFlag.Name)
	snapshotFmt := ctx.String(cannon.RunSnapshotFmtFlag.Name)

	stepFn := us.Step
	if po.cmd != nil {
		stepFn = Guard(po.cmd.ProcessState, stepFn)
	}

	start := time.Now()
	startStep := state.Step

	for !state.Exited {
		if state.Step%100 == 0 { // don't do the ctx err check (includes lock) too often
			if err := ctx.Context.Err(); err != nil {
				return err
			}
		}

		step := state.Step

		if infoAt(state) {
			delta := time.Since(start)
			l.Info("processing",
				"step", step,
				"pc", HexU32(state.PC),
				"insn", HexU32(state.Instr()),
				"ips", float64(step-startStep)/(float64(delta)/float64(time.Second)),
				"pages", state.Memory.PageCount(),
				"mem", state.Memory.Usage(),
				"name", meta.LookupSymbol(state.PC),
			)
		}

		if stopAt(state) {
			break
		}

		if snapshotAt(state) {
			if err := jsonutil.WriteJSON(fmt.Sprintf(snapshotFmt, step), state, OutFilePerm); err != nil {
				return fmt.Errorf("failed to write state snapshot: %w", err)
			}
		}

		prevPreimageOffset := state.PreimageOffset

		if proofAt(state) {
			preStateHash, err := state.EncodeWitness().StateHash()
			if err != nil {
				return fmt.Errorf("failed to hash prestate witness: %w", err)
			}
			witness, err := stepFn(true) // this witness is StepWitness, including pre-state witness, memory proof, and pre-image things
			if err != nil {
				return fmt.Errorf("failed at proof-gen step %d (PC: %08x): %w", step, state.PC, err)
			}
			postStateHash, err := state.EncodeWitness().StateHash()
			if err != nil {
				return fmt.Errorf("failed to hash poststate witness: %w", err)
			}
			proof := &Proof{
				Step:      step,
				Pre:       preStateHash, // hash of pre-state witness
				Post:      postStateHash,
				StateData: witness.State, // pre-state witness
				ProofData: witness.ProofData(),
			}
			if witness.HasPreimage() {
				proof.OracleKey = witness.PreimageKey[:]
				proof.OracleValue = witness.PreimageValue
				proof.OracleOffset = witness.PreimageOffset
			}
			if err := jsonutil.WriteJSON(fmt.Sprintf(proofFmt, step), proof, OutFilePerm); err != nil {
				return fmt.Errorf("failed to write proof data: %w", err)
			}
		} else {
			_, err = stepFn(false)
			if err != nil {
				return fmt.Errorf("failed at step %d (PC: %08x): %w", step, state.PC, err)
			}
		}

		if preimageRead := state.PreimageOffset > prevPreimageOffset; preimageRead {
			if stopAtAnyPreimage {
				break
			}
			if state.PreimageKey[0] == byte(stopAtPreimageTypeByte) {
				break
			}
			if stopAtPreimageLargerThan != 0 && len(us.LastPreimage()) > stopAtPreimageLargerThan {
				break
			}
		}
	}

	// Instruction mix. These counters are incremented in fast.riscvStep and cover the
	// opcode classes the profiler cares about; branches, jumps and system instructions are
	// executed but not counted, so the categories below sum to less than Step.
	share := func(n uint64) string {
		if state.Step == 0 {
			return ""
		}
		return fmt.Sprintf(" (%5.1f%%)", 100*float64(n)/float64(state.Step))
	}
	fmt.Fprintf(os.Stderr, "\n=== instruction profile =========================\n")
	fmt.Fprintf(os.Stderr, "  steps            %12d\n", state.Step)
	fmt.Fprintf(os.Stderr, "  integer ALU      %12d%s\n", state.OpCount.Arith, share(state.OpCount.Arith))
	fmt.Fprintf(os.Stderr, "  memory load/store%12d%s\n", state.OpCount.Mem, share(state.OpCount.Mem))
	fmt.Fprintf(os.Stderr, "  float ALU        %12d%s\n", state.OpCount.Float, share(state.OpCount.Float))
	fmt.Fprintf(os.Stderr, "  float load/store %12d%s\n", state.OpCount.FloatLS, share(state.OpCount.FloatLS))
	fmt.Fprintf(os.Stderr, "  pages touched    %12d  (%s)\n", state.Memory.PageCount(), state.Memory.Usage())
	fmt.Fprintf(os.Stderr, "  wall clock       %12s\n", time.Since(start).Round(time.Millisecond))
	if d := time.Since(start).Seconds(); d > 0 {
		fmt.Fprintf(os.Stderr, "  throughput       %12.2f Minstr/s\n", float64(state.Step-startStep)/d/1e6)
	}
	fmt.Fprintf(os.Stderr, "=================================================\n")

	if err := jsonutil.WriteJSON(ctx.Path(cannon.RunOutputFlag.Name), state, OutFilePerm); err != nil {
		return fmt.Errorf("failed to write state output: %w", err)
	}
	return nil
}

var RunCommand = &cli.Command{
	Name:        "run",
	Usage:       "Execute an MLVM state and optionally emit snapshots or step proofs",
	Description: "Execute an MLVM state. Matcher flags control when to emit a proof or snapshot and when to stop.",
	Action:      Run,
	Flags: []cli.Flag{
		cannon.RunInputFlag,
		cannon.RunOutputFlag,
		cannon.RunProofAtFlag,
		cannon.RunProofFmtFlag,
		cannon.RunSnapshotAtFlag,
		cannon.RunSnapshotFmtFlag,
		cannon.RunStopAtFlag,
		cannon.RunStopAtPreimageTypeFlag,
		cannon.RunStopAtPreimageLargerThanFlag,
		cannon.RunMetaFlag,
		cannon.RunInfoAtFlag,
		cannon.RunPProfCPU,
		LoadRuntimeFileFlag,
		RawOutputFlag,
		TraceFlag,
	},
}
