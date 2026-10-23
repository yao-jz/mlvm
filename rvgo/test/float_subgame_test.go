package test

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yao-jz/mlvm/rvgo/comp"
	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/include"
	"github.com/yao-jz/mlvm/rvgo/slow"
)

// The experimental float terminal route rests on two properties. These test both.
//
//  1. The nested machine computes exactly what MLVM's own SoftFloat computes. If it did
//     not, the nested route would represent a different computation.
//  2. Every instruction of the nested execution verifies through the on-chain verifier —
//     evidence for the terminal mechanism without claiming an outer dispute protocol.

func loadCoprocessor(t *testing.T) *fast.FloatCoprocessor {
	t.Helper()
	path, err := fast.FindFloatCoprocessor()
	if err != nil {
		t.Skip(err.Error())
	}
	c, err := fast.LoadFloatCoprocessor(path)
	require.NoError(t, err)
	return c
}

// TestFloatSubGameMatchesSoftFloat runs the nested machine over the same differential
// vectors the Yul port is checked against, and requires bit-identical results.
//
// The vectors are generated from `rvgo/comp` itself, so this compares three independent
// IEEE-754 implementations: MLVM's Go SoftFloat, Rust's compiler_builtins soft-float inside
// the nested machine, and (in SoftFloat64.t.sol) the Yul port.
func TestFloatSubGameMatchesSoftFloat(t *testing.T) {
	c := loadCoprocessor(t)

	raw, err := os.ReadFile("../../rvsol/test/softfloat-vectors.json")
	if os.IsNotExist(err) {
		t.Skip("no vectors; run: bin/mlvm softfloat-vectors --out rvsol/test/softfloat-vectors.json")
	}
	require.NoError(t, err)

	var vectors struct {
		A   []string `json:"a"`
		B   []string `json:"b"`
		Sum []string `json:"sum"`
	}
	require.NoError(t, json.Unmarshal(raw, &vectors))
	require.Greater(t, len(vectors.A), 1000)

	parse := func(s string) uint64 {
		v, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
		require.NoError(t, err)
		return v
	}

	// The full set is ~1,500 sub-games at ~300 steps each. That is fast enough to run in
	// CI, but the addition cases are what matter here - the other operations get their own
	// coverage below.
	exact, nanPayload, totalSteps := 0, 0, uint64(0)
	for i := range vectors.A {
		a, b, want := parse(vectors.A[i]), parse(vectors.B[i]), parse(vectors.Sum[i])
		got, _, steps, err := c.RunFloatSubGame(fast.FloatOpAdd, a, b, 0)
		require.NoErrorf(t, err, "vector %d: a=%#016x b=%#016x", i, a, b)
		totalSteps += steps
		switch {
		case got == want:
			exact++
		case isNaN(got) && isNaN(want):
			// Both are NaN but the payloads differ. IEEE-754 permits any payload, and the
			// two implementations propagate different ones. This does NOT settle: the
			// sub-game commits to exact bits, so a disputed operation that produces NaN
			// must not be routed here until the payloads are reconciled. Counted, not
			// hidden - see docs/ISA.md.
			nanPayload++
		default:
			t.Fatalf("vector %d: a=%#016x b=%#016x\n  softfloat %#016x\n  sub-game  %#016x",
				i, a, b, want, got)
		}
	}
	t.Logf("%d/%d vectors bit-identical to rvgo/comp", exact, len(vectors.A))
	t.Logf("%d NaN-payload divergences (both NaN, different payload) - not routable to the sub-game", nanPayload)
	t.Logf("mean %d RV64I steps per sub-game", totalSteps/uint64(len(vectors.A)))
	require.Zero(t, nanPayload, "NaN payloads diverge; see the note above before routing NaN operands")
}

// TestFloatSubGameAllOperations sweeps every operation the sub-game routes.
//
// This covers every operation selector currently exposed by the f64 nested route:
// conversions, multiply-add, comparisons, square root, and the basic arithmetic operations.
// It does not imply that every float instruction in a real trace is currently settleable.
//
// The reference is fast.FloatReference, which mirrors fast/vm.go arm for arm. That is the
// point - the co-processor must reproduce what the outer VM did, including where the outer
// VM departs from the standard.
func TestFloatSubGameAllOperations(t *testing.T) {
	c := loadCoprocessor(t)

	// Operands chosen to hit the paths that differ between implementations rather than to
	// sample uniformly: subnormals, exact ties, overflow, the values where truncation and
	// round-to-nearest disagree, and - kept as raw bit patterns because float64 literals
	// cannot express them - the NaNs.
	//
	// The NaNs matter more than they look. FMIN/FMAX return a raw operand rather than a
	// computed value, so the payload survives, and they distinguish quiet from signalling
	// when deciding whether to raise invalid. Arithmetic canonicalises instead. Getting
	// those two behaviours the wrong way round is exactly the kind of thing that passes
	// every test written without NaN operands.
	floatBits := []uint64{
		math.Float64bits(0.0), math.Float64bits(math.Copysign(0, -1)),
		math.Float64bits(1.0), math.Float64bits(-1.0), math.Float64bits(2.0),
		math.Float64bits(0.5), math.Float64bits(math.Pi), math.Float64bits(math.E),
		math.Float64bits(0.1), math.Float64bits(0.3),
		math.Float64bits(2.5), math.Float64bits(-2.5), math.Float64bits(2.6),
		math.Float64bits(-2.6), math.Float64bits(1.5), math.Float64bits(3.5),
		math.Float64bits(5e-324), math.Float64bits(1e-300), math.Float64bits(1e300),
		math.Float64bits(math.MaxFloat64), math.Float64bits(math.SmallestNonzeroFloat64),
		math.Float64bits(math.Inf(1)), math.Float64bits(math.Inf(-1)),
		math.Float64bits(1e18), math.Float64bits(-1e18), math.Float64bits(1e19),
		math.Float64bits(4294967295.0), math.Float64bits(2147483648.0),
		math.Float64bits(-2147483649.0),
		0x7FF8000000000000, // canonical quiet NaN
		0x7FF8000000BEEF00, // quiet NaN carrying a payload
		0xFFF8000000000000, // negative quiet NaN
		0x7FF0000000000001, // signalling NaN
		0xFFF0DEADBEEF0001, // negative signalling NaN
	}
	ints := []int64{
		0, 1, -1, 2, -2, 12345, -12345, 1 << 31, -(1 << 31), (1 << 31) - 1,
		1 << 52, 1<<53 + 1, 1 << 62, math.MaxInt64, math.MinInt64, -1234567890123,
	}

	type opSpec struct {
		op     uint64
		ternry bool
		intIn  bool
	}
	specs := []opSpec{
		{op: fast.FloatOpAdd}, {op: fast.FloatOpSub}, {op: fast.FloatOpMul}, {op: fast.FloatOpDiv},
		{op: fast.FloatOpMin}, {op: fast.FloatOpMax}, {op: fast.FloatOpSqrt},
		{op: fast.FloatOpMAdd, ternry: true}, {op: fast.FloatOpMSub, ternry: true},
		{op: fast.FloatOpNMSub, ternry: true}, {op: fast.FloatOpNMAdd, ternry: true},
		{op: fast.FloatOpCvtDW, intIn: true}, {op: fast.FloatOpCvtDWU, intIn: true},
		{op: fast.FloatOpCvtDL, intIn: true}, {op: fast.FloatOpCvtDLU, intIn: true},
		{op: fast.FloatOpCvtWD}, {op: fast.FloatOpCvtWUD},
		{op: fast.FloatOpCvtLD}, {op: fast.FloatOpCvtLUD},
		{op: fast.FloatOpEq}, {op: fast.FloatOpLt}, {op: fast.FloatOpLe},
	}

	for _, spec := range specs {
		t.Run(fast.FloatOpName(spec.op), func(t *testing.T) {
			var cases [][3]uint64
			switch {
			case spec.intIn:
				for _, v := range ints {
					cases = append(cases, [3]uint64{uint64(v), 0, 0})
				}
			case spec.ternry:
				// The fused multiply-add is the hardest operation to get right - one rounding
				// over a 106-bit product plus a third operand - so vary all three rather than
				// pinning c, and include the combinations where the product underflows while
				// c does not. That pairing is what broke the first implementation.
				for _, x := range floatBits {
					for _, y := range floatBits {
						for _, z := range []uint64{
							math.Float64bits(1.5), math.Float64bits(-1.5), 0,
							math.Float64bits(math.Copysign(0, -1)),
							math.Float64bits(5e-324), math.Float64bits(1e300),
							math.Float64bits(math.Pi), 0x7FF0000000000001,
						} {
							cases = append(cases, [3]uint64{x, y, z})
						}
					}
				}
			default:
				for _, x := range floatBits {
					for _, y := range floatBits {
						cases = append(cases, [3]uint64{x, y, 0})
					}
				}
			}

			var steps uint64
			for _, in := range cases {
				got, gotFlags, n, err := c.RunFloatSubGame(spec.op, in[0], in[1], in[2])
				require.NoErrorf(t, err, "a=%#016x b=%#016x c=%#016x", in[0], in[1], in[2])
				want, wantFlags, ok := fast.FloatReferenceWithFlags(spec.op, in[0], in[1], in[2])
				require.True(t, ok, "no reference for %s", fast.FloatOpName(spec.op))
				if got != want {
					t.Fatalf("%s(a=%#016x, b=%#016x, c=%#016x)\n  rvgo/comp %#016x\n  sub-game  %#016x",
						fast.FloatOpName(spec.op), in[0], in[1], in[2], want, got)
				}
				// The flags are committed state too: a float instruction updates fcsr as well
				// as its destination register, so proving only the value would leave half the
				// transition unsettled.
				if gotFlags != uint64(wantFlags) {
					t.Fatalf("%s(a=%#016x, b=%#016x, c=%#016x) flags\n  rvgo/comp %s\n  sub-game  %s",
						fast.FloatOpName(spec.op), in[0], in[1], in[2],
						flagNames(uint64(wantFlags)), flagNames(gotFlags))
				}
				steps += n
			}
			t.Logf("%d operand sets, value and flags bit-identical; mean %d RV64I steps",
				len(cases), steps/uint64(len(cases)))
		})
	}
}

// TestFloatSubGameOnChain checks every instruction of one nested float operation through
// RISCV.sol running in a real EVM.
//
// This validates the terminal integer trace. It does not bind the operands to an outer
// witness or implement the interactive game that would establish the nested post-state.
func TestFloatSubGameOnChain(t *testing.T) {
	if _, err := os.Stat("../../rvsol/out/RISCV.sol/RISCV.json"); os.IsNotExist(err) {
		t.Skip("no forge artifacts; run `forge build` in rvsol first")
	}
	c := loadCoprocessor(t)

	a := math.Float64bits(math.Pi)
	b := math.Float64bits(math.E)
	want := uint64(comp.F64_add(include.Float64_t(a), include.Float64_t(b)))

	state := c.NewFloatSubGame(fast.FloatOpAdd, a, b, 0)
	preHash, err := state.EncodeWitness().StateHash()
	require.NoError(t, err)

	env := newEVMEnv(t, testContracts(t), testAddrs)
	in := fast.NewInstrumentedState(state, nil, os.Stdout, os.Stderr)

	var maxGas, totalGas uint64
	steps := uint64(0)
	for i := uint64(0); i < 10_000 && !state.Exited; i++ {
		wit, err := in.Step(true)
		require.NoErrorf(t, err, "fast VM at nested step %d", i)

		fastHash, err := state.EncodeWitness().StateHash()
		require.NoError(t, err)

		slowHash, err := slow.Step(wit.EncodeStepInput(fast.LocalContext{}), nil)
		require.NoErrorf(t, err, "slow VM at nested step %d, PC %#x", i, state.PC)
		require.Equalf(t, fastHash, slowHash, "slow must match fast at nested step %d", i)

		_, evmHash, gas := stepEVM(t, env, wit, testAddrs, i)
		require.Equalf(t, fastHash, evmHash, "EVM must match fast at nested step %d", i)
		if gas > maxGas {
			maxGas = gas
		}
		totalGas += gas
		steps++
	}

	require.True(t, state.Exited, "nested machine ran out of steps")
	require.Zero(t, state.ExitCode)
	require.Equal(t, want, fast.FloatSubGameResult(state), "sub-game result must match SoftFloat")

	postHash, err := state.EncodeWitness().StateHash()
	require.NoError(t, err)

	t.Logf("FADD.D(pi, e) reproduced by the nested machine and EVM stepper")
	t.Logf("  nested pre-state   %x", preHash)
	t.Logf("  nested post-state  %x", postHash)
	t.Logf("  steps              %d   (bisection depth %d)", steps, bitLen(steps))
	t.Logf("  result             %#016x", fast.FloatSubGameResult(state))
	t.Logf("  gas: max %d per checked step, %d for this full test trace", maxGas, totalGas)
	t.Logf("  a future terminal game could execute one such step; protocol overhead is excluded")
}

// TestFloatSubGameIsDeterministic checks the property the sub-game depends on: two
// independent derivations from the same inputs produce the same committed state.
//
// If this ever failed, two honest parties could disagree about what they were disputing.
func TestFloatSubGameIsDeterministic(t *testing.T) {
	c := loadCoprocessor(t)
	a, b := math.Float64bits(math.Pi), math.Float64bits(math.E)

	h1, err := c.NewFloatSubGame(fast.FloatOpAdd, a, b, 0).EncodeWitness().StateHash()
	require.NoError(t, err)
	h2, err := c.NewFloatSubGame(fast.FloatOpAdd, a, b, 0).EncodeWitness().StateHash()
	require.NoError(t, err)
	require.Equal(t, h1, h2, "the same inputs must give the same nested pre-state")

	// And different inputs must give a different one, or the commitment says nothing.
	h3, err := c.NewFloatSubGame(fast.FloatOpAdd, a, b+1, 0).EncodeWitness().StateHash()
	require.NoError(t, err)
	require.NotEqual(t, h1, h3, "different operands must give a different nested pre-state")

	h4, err := c.NewFloatSubGame(fast.FloatOpMul, a, b, 0).EncodeWitness().StateHash()
	require.NoError(t, err)
	require.NotEqual(t, h1, h4, "a different operation must give a different nested pre-state")

	t.Logf("co-processor image root %x", c.ImageRoot())
	t.Logf("FADD.D(pi, e) pre-state %x", h1)
}

func isNaN(bits uint64) bool {
	return bits&0x7FF0000000000000 == 0x7FF0000000000000 && bits&0x000FFFFFFFFFFFFF != 0
}

func bitLen(v uint64) int {
	n := 0
	for v > 0 {
		n++
		v >>= 1
	}
	return n
}

// flagNames renders an fflags word the way the RISC-V spec names the bits, so a differential
// failure says "NX|UF" rather than "3".
func flagNames(f uint64) string {
	if f == 0 {
		return "(none)"
	}
	out := ""
	for _, b := range []struct {
		bit  uint64
		name string
	}{{1, "NX"}, {2, "UF"}, {4, "OF"}, {8, "DZ"}, {16, "NV"}} {
		if f&b.bit != 0 {
			if out != "" {
				out += "|"
			}
			out += b.name
		}
	}
	return out
}
