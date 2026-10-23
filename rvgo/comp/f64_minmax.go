package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

// FMIN and FMAX are RISC-V specializations, not Berkeley SoftFloat operations, and their
// semantics differ from a plain comparison in three ways that all matter:
//
//   - **NaN is absorbing in the other direction.** They return the *non-NaN* operand when
//     exactly one input is NaN, and the canonical NaN only when both are. A naive
//     `a < b ? a : b` propagates the NaN instead, which is what MLVM used to do.
//   - **Only a signaling NaN is invalid.** A quiet NaN operand is not an exception here,
//     unlike in FLT/FLE. Routing through f64_lt raised invalid on any NaN.
//   - **Zeros are ordered.** -0.0 compares less than +0.0, even though they are numerically
//     equal, so FMIN(-0.0, +0.0) is -0.0 and FMAX is +0.0. A comparison-based
//     implementation returns whichever operand the tie-break happens to pick.
//
// RISC-V unprivileged spec, "Single-Precision Floating-Point Computational Instructions".

// F64_min implements FMIN.D.
func F64_min(a, b Float64_t) Float64_t {
	if pick, ok := minMaxNaN64(a, b); ok {
		return pick
	}
	if bothZeroF64(a, b) {
		// Equal in value, ordered by sign: the negative zero is the minimum.
		if SignF64UI(uint64(a)) {
			return a
		}
		return b
	}
	if F64_lt(a, b) {
		return a
	}
	return b
}

// F64_max implements FMAX.D.
func F64_max(a, b Float64_t) Float64_t {
	if pick, ok := minMaxNaN64(a, b); ok {
		return pick
	}
	if bothZeroF64(a, b) {
		if SignF64UI(uint64(a)) {
			return b
		}
		return a
	}
	if F64_lt(a, b) {
		return b
	}
	return a
}

// minMaxNaN64 applies the shared NaN rule and reports whether it decided the result.
func minMaxNaN64(a, b Float64_t) (Float64_t, bool) {
	uiA, uiB := uint64(a), uint64(b)
	// Signaling NaN is the only invalid operation here; a quiet NaN passes silently.
	if Softfloat_isSigNaNF64UI(uiA) || Softfloat_isSigNaNF64UI(uiB) {
		Softfloat_raiseFlags(Softfloat_flag_invalid)
	}
	aNaN, bNaN := IsNaNF64UI(uiA), IsNaNF64UI(uiB)
	switch {
	case aNaN && bNaN:
		return Float64_t(0x7FF8000000000000), true
	case aNaN:
		return b, true
	case bNaN:
		return a, true
	}
	return 0, false
}

func bothZeroF64(a, b Float64_t) bool {
	return uint64(a)&0x7FFFFFFFFFFFFFFF == 0 && uint64(b)&0x7FFFFFFFFFFFFFFF == 0
}
