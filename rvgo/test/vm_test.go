package test

import (
	"debug/elf"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/slow"
)

func forEachTestSuite(t *testing.T, path string, callItem func(t *testing.T, path string)) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("missing tests: %s", path)
	} else {
		require.NoError(t, err, "failed to stat path")
	}
	items, err := os.ReadDir(path)
	require.NoError(t, err, "failed to read dir items")
	require.NotEmpty(t, items, "expected at least one test suite binary")

	for _, item := range items {
		if !item.IsDir() && !strings.HasSuffix(item.Name(), ".dump") {
			t.Run(item.Name(), func(t *testing.T) {
				callItem(t, filepath.Join(path, item.Name()))
			})
		}
	}
}

func runFastTestSuite(t *testing.T, path string) {
	testSuiteELF, err := elf.Open(path)
	require.NoError(t, err)
	defer testSuiteELF.Close()

	vmState, err := fast.LoadELF(testSuiteELF)
	require.NoError(t, err, "must load test suite ELF binary")

	inState := fast.NewInstrumentedState(vmState, nil, os.Stdout, os.Stderr)

	for i := 0; i < 10_000; i++ {
		//fmt.Printf("pc: 0x%x\n", vmState.PC)
		if _, err := inState.Step(false); err != nil {
			t.Fatalf("VM err at step %d, PC %x: %v", i, vmState.PC, err)
		}
		if vmState.Exited {
			break
		}
	}
	require.True(t, vmState.Exited, "ran out of steps")
	if vmState.ExitCode != 0 {
		testCaseNum := vmState.ExitCode >> 1
		t.Fatalf("failed at test case %d", testCaseNum)
	}
}

func runSlowTestSuite(t *testing.T, path string) {
	testSuiteELF, err := elf.Open(path)
	require.NoError(t, err)
	defer testSuiteELF.Close()

	vmState, err := fast.LoadELF(testSuiteELF)
	require.NoError(t, err, "must load test suite ELF binary")

	instState := fast.NewInstrumentedState(vmState, nil, nil, nil)

	for i := 0; i < 10_000; i++ {
		//t.Logf("next step - pc: 0x%x\n", vmState.PC)

		wit, err := instState.Step(true)
		require.NoError(t, err)

		// Now run the same in slow mode
		input := wit.EncodeStepInput(fast.LocalContext{})
		post, err := slow.Step(input, nil)
		require.NoErrorf(t, err, "slow VM err at step %d, PC %08x: %v", i, vmState.PC, err)

		fastPostState := vmState.EncodeWitness()
		fastRoot, err := fastPostState.StateHash()
		require.NoError(t, err)
		if post != fastRoot {
			t.Fatalf("slow state %x must match fast state %x", post, fastRoot)
		}

		if vmState.Exited {
			break
		}
	}

	require.True(t, vmState.Exited, "ran out of steps")
	if vmState.ExitCode != 0 {
		testCaseNum := vmState.ExitCode >> 1
		t.Fatalf("failed at test case %d", testCaseNum)
	}
}

func runEVMTestSuite(t *testing.T, path string) {
	contracts := testContracts(t)
	addrs := testAddrs
	env := newEVMEnv(t, contracts, addrs)
	//addTracer(t, env, addrs, contracts)

	testSuiteELF, err := elf.Open(path)
	require.NoError(t, err)
	defer testSuiteELF.Close()

	vmState, err := fast.LoadELF(testSuiteELF)
	require.NoError(t, err, "must load test suite ELF binary")

	instState := fast.NewInstrumentedState(vmState, nil, nil, nil)

	maxGasUsed := uint64(0)

	for i := uint64(0); i < 10_000; i++ {
		//t.Logf("next step - pc: 0x%x\n", vmState.PC)

		wit, err := instState.Step(true)
		require.NoError(t, err)

		evmPost, evmPostHash, gasUsed := stepEVM(t, env, wit, addrs, i)
		if gasUsed > maxGasUsed {
			maxGasUsed = gasUsed
		}

		fastPostState := vmState.EncodeWitness()
		fastStateHash, err := fastPostState.StateHash()
		require.NoError(t, err)
		if evmPostHash != fastStateHash {
			t.Fatalf("evm state %x must match fast state %x\nfast:%x\nevm: %x\n", evmPostHash, fastStateHash, fastPostState, evmPost)
		}

		if vmState.Exited {
			break
		}
	}

	t.Logf("max gas used: %d", maxGasUsed)

	require.True(t, vmState.Exited, "ran out of steps")
	if vmState.ExitCode != 0 {
		testCaseNum := vmState.ExitCode >> 1
		t.Fatalf("failed at test case %d", testCaseNum)
	}
}

func TestFastStep(t *testing.T) {
	testsPath := filepath.FromSlash("../../tests/riscv-tests")
	runTestCategory := func(name string) {
		t.Run(name, func(t *testing.T) {
			forEachTestSuite(t, filepath.Join(testsPath, name), runFastTestSuite)
		})
	}
	runTestCategory("rv64ui-p")
	runTestCategory("rv64um-p")
	runTestCategory("rv64ua-p")
	//runTestCategory("benchmarks")  TODO benchmarks (fix ELF bench data loading and wrap in Go benchmark?)
}

// TestFastStepFD runs the F and D (single/double precision floating point) test-suites
// against the fast VM, which implements them via the Go port of Berkeley SoftFloat in
// rvgo/comp.
//
// Three outcomes are possible per suite, and it matters which is which:
//
//   - pass          the suite ran its cases and all of them agreed
//   - known failure a documented semantic gap; asserted to STILL fail, so that both
//     regressions and fixes show up. When you fix one, delete its entry.
//   - self-skip     an rv32u[fd] suite running on an RV64 machine. These begin with an
//     XLEN probe (`li a0,1; slli a0,a0,31; bltz a0, <body>`) that is only
//     taken when XLEN==32, so on RV64 they jump straight to exit(0) after
//     ~68 steps without executing a single float instruction. They exit
//     cleanly, but counting them as passes would badly overstate coverage.
func TestFastStepFD(t *testing.T) {
	testsPath := filepath.FromSlash("../../tests/riscv-tests")
	t.Run("rv64fd", func(t *testing.T) {
		forEachTestSuite(t, filepath.Join(testsPath, "rv64fd"), func(t *testing.T, path string) {
			name := filepath.Base(path)
			switch {
			case strings.HasPrefix(name, "rv32u"):
				runSelfSkippingTestSuite(t, path)
			case fdKnownFailures[name] != "":
				runExpectedFailureTestSuite(t, path, fdKnownFailures[name])
			default:
				runFastTestSuite(t, path)
			}
		})
	})
}

// selfSkipStepLimit is the number of steps an rv32 suite takes to reach its XLEN probe and
// exit. A suite that runs materially longer than this executed real test cases, which
// means our understanding of it is wrong and the result should not be trusted.
const selfSkipStepLimit = 100

// runSelfSkippingTestSuite asserts that an rv32u[fd] suite behaves the way we believe it
// does on an RV64 machine: exit cleanly, almost immediately, having tested nothing.
func runSelfSkippingTestSuite(t *testing.T, path string) {
	testSuiteELF, err := elf.Open(path)
	require.NoError(t, err)
	defer testSuiteELF.Close()

	vmState, err := fast.LoadELF(testSuiteELF)
	require.NoError(t, err, "must load test suite ELF binary")

	inState := fast.NewInstrumentedState(vmState, nil, io.Discard, io.Discard)
	for i := 0; i < 10_000 && !vmState.Exited; i++ {
		_, err := inState.Step(false)
		require.NoErrorf(t, err, "VM err at step %d, PC %x", i, vmState.PC)
	}
	require.True(t, vmState.Exited, "ran out of steps")
	require.Zero(t, vmState.ExitCode, "self-skipping suite must still exit cleanly")
	require.LessOrEqualf(t, vmState.Step, uint64(selfSkipStepLimit),
		"%s ran %d steps: it is executing real test cases on RV64, not self-skipping. "+
			"Reclassify it rather than counting it as coverage.", filepath.Base(path), vmState.Step)
	t.Logf("self-skip: rv32 suite exited after %d steps without running float cases (XLEN probe)", vmState.Step)
}

// fdKnownFailures maps a floating-point test-suite name to the reason it does not pass
// yet, verified against the disassembly in tests/riscv-tests/rv64fddmp.
//
// Empty, and that is the point: every applicable floating-point conformance suite passes.
//
// It held 14 entries. Eleven were one root cause - fcsr/fflags/frm hardwired to zero, so any
// test asserting an IEEE-754 exception failed even though the computed value was bit-exact.
// The last three were FMIN/FMAX NaN selection and NaN-boxing.
//
// Keep the mechanism rather than deleting it. runExpectedFailureTestSuite asserts a listed
// suite *still* fails, so a regression that makes one pass shows up as a test error telling
// you to remove the entry - and the same check is what caught each fix as it landed.
var fdKnownFailures = map[string]string{}

// runExpectedFailureTestSuite asserts that a suite still fails, and reports loudly if it
// started passing so the allow-list entry can be removed.
func runExpectedFailureTestSuite(t *testing.T, path string, reason string) {
	testSuiteELF, err := elf.Open(path)
	require.NoError(t, err)
	defer testSuiteELF.Close()

	vmState, err := fast.LoadELF(testSuiteELF)
	require.NoError(t, err, "must load test suite ELF binary")

	inState := fast.NewInstrumentedState(vmState, nil, io.Discard, io.Discard)

	var stepErr error
	for i := 0; i < 10_000; i++ {
		if _, stepErr = inState.Step(false); stepErr != nil {
			break
		}
		if vmState.Exited {
			break
		}
	}
	switch {
	case stepErr != nil:
		t.Logf("known failure (%s): VM error at PC %x: %v", reason, vmState.PC, stepErr)
	case !vmState.Exited:
		t.Logf("known failure (%s): ran out of steps", reason)
	case vmState.ExitCode != 0:
		t.Logf("known failure (%s): failed at test case %d", reason, vmState.ExitCode>>1)
	default:
		t.Errorf("suite now passes - remove %q from fdKnownFailures (was: %s)", filepath.Base(path), reason)
	}
}

// TestSlowStep cross-checks every fast-mode step against the state-oracle ("slow") VM,
// which is the 1:1 Go mirror of rvsol/src/RISCV.sol. Both must produce the same post-state
// hash for every instruction in the conformance suites.
//
// Slow mode carries the full 674-byte witness but does not execute F or D
// instructions; the float registers pass through untouched. That is sufficient for any
// guest that contains no floating point, which is why the integer, multiply and atomic
// suites cross-check here while a float program would not.
// See docs/ARCHITECTURE.md#three-vms-one-semantics.
func TestSlowStep(t *testing.T) {
	testsPath := filepath.FromSlash("../../tests/riscv-tests")
	runTestCategory := func(name string) {
		t.Run(name, func(t *testing.T) {
			forEachTestSuite(t, filepath.Join(testsPath, name), runSlowTestSuite)
		})
	}
	runTestCategory("rv64ui-p")
	runTestCategory("rv64um-p")
	runTestCategory("rv64ua-p")
	//runTestCategory("benchmarks")  TODO benchmarks (fix ELF bench data loading and wrap in Go benchmark?)
}

// TestEVMStep replays every step inside a real EVM against rvsol/src/RISCV.sol and
// compares the EVM post-state hash with the fast VM's. This checks the terminal transition
// property a complete dispute protocol would depend on; it does not implement that protocol.
//
// Needs `forge build` artifacts in rvsol/out; the suite skips if they are absent.
func TestEVMStep(t *testing.T) {
	if _, err := os.Stat("../../rvsol/out/RISCV.sol/RISCV.json"); os.IsNotExist(err) {
		t.Skip("no forge artifacts; run `forge build` in rvsol first")
	}
	testsPath := filepath.FromSlash("../../tests/riscv-tests")
	runTestCategory := func(name string) {
		t.Run(name, func(t *testing.T) {
			forEachTestSuite(t, filepath.Join(testsPath, name), runEVMTestSuite)
		})
	}
	runTestCategory("rv64ui-p")
	runTestCategory("rv64um-p")
	runTestCategory("rv64ua-p")
	//runTestCategory("benchmarks")  TODO benchmarks (fix ELF bench data loading and wrap in Go benchmark?)
}
