package test

import (
	"bytes"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yao-jz/mlvm/rvgo/comp"
	"github.com/yao-jz/mlvm/rvgo/fast"
	"github.com/yao-jz/mlvm/rvgo/include"
)

// MLVM's SoftFloat is a hand port of Berkeley SoftFloat 3 from C to Go, and hand ports of
// branchy bit manipulation are exactly the kind of code that looks right and is not.
//
// Everything else that checks it compares against another implementation that could share a
// mistake, or against expected values that were themselves produced by it. This compares
// against the host FPU, which is an independent, correctly-rounded implementation of the same
// standard - so a disagreement means one of them is wrong, and for +, -, *, / and sqrt the
// correctly-rounded result is unique, so it is not a matter of taste.
//
// It is here because two real bugs got through without it:
//
//   - F64_div dropped a `(uint32_t)` truncation from the C, producing a quotient one ULP off
//     on about 2% of inputs. Every conformance suite passed, `make determinism` passed, and
//     five transformer models produced plausible output.
//   - F64_to_ui64 never assigned its operand, so it decomposed the constant zero and returned
//     0 for every input. No conformance suite exercises FCVT.LU.D.
//
// Both were found by the float sub-game's differential test, which compares MLVM against
// Rust's compiler_builtins. This test is the cheaper, more direct version of that check.
func TestSoftFloatAgainstNativeFPU(t *testing.T) {
	// Skewed hard toward the cases that break implementations: subnormals, huge exponent
	// differences, exact ties. A uniform sample of 64-bit patterns is almost entirely NaN
	// and would exercise almost nothing.
	operand := func(r *rand.Rand) float64 {
		switch r.Intn(5) {
		case 0:
			return r.NormFloat64()
		case 1:
			return math.Float64frombits(r.Uint64() & 0x7FEFFFFFFFFFFFFF) // any finite magnitude
		case 2:
			return r.NormFloat64() * math.Pow(2, float64(r.Intn(600)-300)) // wide exponent range
		case 3:
			return math.Float64frombits(uint64(r.Intn(1 << 20))) // subnormal
		}
		return float64(r.Intn(1<<20)) / float64(int64(1)<<uint(r.Intn(20))) // exact binary fractions
	}

	const samples = 200_000
	for _, op := range []struct {
		name   string
		soft   func(a, b uint64) uint64
		native func(a, b float64) float64
	}{
		{"F64_add",
			func(a, b uint64) uint64 {
				return uint64(comp.F64_add(include.Float64_t(a), include.Float64_t(b)))
			},
			func(a, b float64) float64 { return a + b }},
		{"F64_sub",
			func(a, b uint64) uint64 {
				return uint64(comp.F64_sub(include.Float64_t(a), include.Float64_t(b)))
			},
			func(a, b float64) float64 { return a - b }},
		{"F64_mul",
			func(a, b uint64) uint64 {
				return uint64(comp.F64_mul(include.Float64_t(a), include.Float64_t(b)))
			},
			func(a, b float64) float64 { return a * b }},
		{"F64_div",
			func(a, b uint64) uint64 {
				return uint64(comp.F64_div(include.Float64_t(a), include.Float64_t(b)))
			},
			func(a, b float64) float64 { return a / b }},
		{"F64_sqrt",
			func(a, b uint64) uint64 {
				return uint64(comp.F64_sqrt(include.Float64_t(a)))
			},
			func(a, b float64) float64 { return math.Sqrt(a) }},
	} {
		t.Run(op.name, func(t *testing.T) {
			r := rand.New(rand.NewSource(1)) // fixed seed: a failure must be reproducible
			checked := 0
			for i := 0; i < samples; i++ {
				x, y := operand(r), operand(r)
				want := op.native(x, y)
				if math.IsNaN(want) {
					// NaN payloads are permitted to differ, and MLVM canonicalises where
					// RISC-V requires it. Not a correctness question, so skip.
					continue
				}
				got := op.soft(math.Float64bits(x), math.Float64bits(y))
				if got != math.Float64bits(want) {
					t.Fatalf("%s(%#016x, %#016x)\n  softfloat %#016x  (%v)\n  host FPU  %#016x  (%v)",
						op.name, math.Float64bits(x), math.Float64bits(y),
						got, math.Float64frombits(got), math.Float64bits(want), want)
				}
				checked++
			}
			t.Logf("%d operand pairs, bit-identical to the host FPU", checked)
		})
	}
}

// TestSoftFloatConversionsAgainstNativeFPU covers the FCVT paths, which the arithmetic sweep
// above does not reach and which no conformance suite exercises either.
//
// Go's conversions are not a drop-in oracle here: `float64 -> int64` truncates toward zero,
// while SoftFloat rounds to nearest with ties to even, and the two disagree on out-of-range
// and NaN inputs by design. So this checks the cases where they must agree - values already
// integral and in range - which is enough to catch a function that ignores its operand.
func TestSoftFloatConversionsAgainstNativeFPU(t *testing.T) {
	integral := []float64{
		0, 1, -1, 2, -2, 3, 100, -100, 12345, -12345,
		1 << 20, 1 << 30, 1 << 52, -(1 << 52),
		2147483647, -2147483648, 4294967295,
		1e15, -1e15, 1e18,
	}

	for _, v := range integral {
		bits := include.Float64_t(math.Float64bits(v))

		if v >= math.MinInt64 && v <= math.MaxInt64 {
			require.Equalf(t, int64(v), comp.F64_to_i64(bits, true), "F64_to_i64(%v)", v)
		}
		if v >= 0 && v <= math.MaxInt64 {
			require.Equalf(t, uint64(v), comp.F64_to_ui64(bits, true), "F64_to_ui64(%v)", v)
		}
		if v >= math.MinInt32 && v <= math.MaxInt32 {
			require.Equalf(t, int32(v), comp.F64_to_i32(bits, true), "F64_to_i32(%v)", v)
		}
		if v >= 0 && v <= math.MaxUint32 {
			require.Equalf(t, uint32(v), comp.F64_to_ui32(bits, true), "F64_to_ui32(%v)", v)
		}
	}

	// And the other direction, where the conversion is exact for every value that fits.
	for _, v := range []int64{0, 1, -1, 2, -2, 12345, -12345, 1 << 31, -(1 << 31), 1 << 52} {
		require.Equalf(t, math.Float64bits(float64(v)), uint64(comp.I64_to_f64(v)), "I64_to_f64(%d)", v)
		require.Equalf(t, math.Float64bits(float64(uint64(v))), uint64(comp.UI64ToF64(uint64(v))), "UI64ToF64(%d)", v)
		if v >= math.MinInt32 && v <= math.MaxInt32 {
			require.Equalf(t, math.Float64bits(float64(int32(v))), uint64(comp.I32_to_f64(int32(v))), "I32_to_f64(%d)", v)
		}
	}
	t.Logf("%d integral values, conversions exact in both directions", len(integral))
}

// TestSetMemoryRangeInvalidatesRoot pins a cache-invalidation bug that made a merkle root go
// stale.
//
// Memory keeps two layers of cached hashes: the nodes inside a page, and the nodes between a
// page and the root. SetUnaligned — the path the VM's own stores take — drops both.
// SetMemoryRange dropped only the first, so a root computed before it was served again
// afterwards, as if the write had not happened.
//
// It went unnoticed because every caller happened to run at setup, before any root was taken.
// It surfaced when TestMLClaimReadback tampered with a committed answer after the fact and
// the state hash did not move — which, had it been real, is precisely the forgery the
// commitment exists to prevent.
func TestSetMemoryRangeInvalidatesRoot(t *testing.T) {
	m := fast.NewMemory()
	require.NoError(t, m.SetMemoryRange(0x4000, bytes.NewReader([]byte{1, 2, 3, 4})))

	before := m.MerkleRoot() // take a root, so the branch above the page is cached

	require.NoError(t, m.SetMemoryRange(0x4000, bytes.NewReader([]byte{9})))
	after := m.MerkleRoot()
	require.NotEqual(t, before, after, "writing through SetMemoryRange must move the root")

	// And the new root must be the one a freshly built memory with the same contents gives,
	// not merely a different one.
	fresh := fast.NewMemory()
	require.NoError(t, fresh.SetMemoryRange(0x4000, bytes.NewReader([]byte{9, 2, 3, 4})))
	require.Equal(t, fresh.MerkleRoot(), after, "the root must reflect the actual bytes")
}
