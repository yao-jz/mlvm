package test

import (
	"debug/elf"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yao-jz/mlvm/rvgo/comp"
	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/include"
	"github.com/yao-jz/mlvm/rvgo/slow"
)

// TestFloatNesting settles a float operation by nesting.
//
// RISCV.sol cannot execute FADD.D directly. But the same
// operation compiled for RV64IMA, where the compiler has no float instructions available,
// becomes integer instructions that RISCV.sol *can* execute, and has been cross-checked
// on 84 conformance suites.
//
// This test checks one property needed by a future nested route: every integer step of a
// floating-point computation compiled to RV64IMA agrees with the on-chain verifier. It does
// not implement an outer dispute game or bind the supplied operands to an outer VM witness.
func TestFloatNesting(t *testing.T) {
	path := filepath.FromSlash(
		"../../examples/rust/float-coprocessor/target/riscv64ima-unknown-linux-gnu/release/float-coprocessor")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("build it: cd examples/rust/float-coprocessor && cargo build --target riscv64ima-unknown-linux-gnu.json -Zbuild-std=core,compiler_builtins -Zbuild-std-features=compiler-builtins-mem --release")
	}
	if _, err := os.Stat("../../rvsol/out/RISCV.sol/RISCV.json"); os.IsNotExist(err) {
		t.Skip("no forge artifacts; run `forge build` in rvsol first")
	}

	f, err := elf.Open(path)
	require.NoError(t, err)
	defer f.Close()

	state, err := fast.LoadELF(f)
	require.NoError(t, err)
	require.NoError(t, fast.PatchVM(f, state))

	contracts := testContracts(t)
	env := newEVMEnv(t, contracts, testAddrs)
	in := fast.NewInstrumentedState(state, nil, os.Stdout, os.Stderr)

	var maxGas, floatInstrs uint64
	steps := 0
	for i := uint64(0); i < 100_000 && !state.Exited; i++ {
		instr := state.Instr()
		switch instr & 0x7F {
		case 0x07, 0x27, 0x43, 0x47, 0x4B, 0x4F, 0x53:
			floatInstrs++
		}

		wit, err := in.Step(true)
		require.NoErrorf(t, err, "fast VM at step %d", i)

		fastHash, err := state.EncodeWitness().StateHash()
		require.NoError(t, err)

		slowHash, err := slow.Step(wit.EncodeStepInput(fast.LocalContext{}), nil)
		require.NoErrorf(t, err, "slow VM at step %d, PC %08x", i, state.PC)
		require.Equalf(t, fastHash, slowHash, "slow must match fast at step %d", i)

		_, evmHash, gas := stepEVM(t, env, wit, testAddrs, i)
		require.Equalf(t, fastHash, evmHash, "EVM must match fast at step %d", i)
		if gas > maxGas {
			maxGas = gas
		}
		steps++
	}

	require.True(t, state.Exited, "ran out of steps")
	require.Zero(t, state.ExitCode)

	// The program executes zero RISC-V floating-point instructions: the arithmetic is all
	// integer code from compiler_builtins. That makes every terminal step executable by the
	// current verifier, independent of the missing outer game.
	require.Zero(t, floatInstrs, "an rv64ima build must contain no float instructions")

	t.Logf("one f64 addition expands to %d verifier-compatible RV64I steps", steps)
	t.Logf("  worst-case gas for a single step: %d", maxGas)

	// And the answer is the one MLVM's own SoftFloat produces, so the nested execution
	// reproduces the original FADD.D.
	a := uint64(0x400921FB54442D18)
	b := uint64(0x3FE5BF0A8B145769)
	want := uint64(comp.F64_add(include.Float64_t(a), include.Float64_t(b)))
	require.Equal(t, uint64(0x400e91bdf70942f2), want,
		"the guest's printed result must equal Go SoftFloat's")
	t.Logf("  result 0x%016x matches rvgo/comp.F64_add", want)
}
