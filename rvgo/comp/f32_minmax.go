package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

// The single-precision twins of F64_min / F64_max; see f64_minmax.go for why these are
// not a plain comparison.

// F32_min implements FMIN.S.
func F32_min(a, b Float32_t) Float32_t {
	if pick, ok := minMaxNaN32(a, b); ok {
		return pick
	}
	if bothZeroF32(a, b) {
		// Equal in value, ordered by sign: the negative zero is the minimum.
		if SignF32UI(uint32(a)) {
			return a
		}
		return b
	}
	if F32_lt(a, b) {
		return a
	}
	return b
}

// F32_max implements FMAX.S.
func F32_max(a, b Float32_t) Float32_t {
	if pick, ok := minMaxNaN32(a, b); ok {
		return pick
	}
	if bothZeroF32(a, b) {
		if SignF32UI(uint32(a)) {
			return b
		}
		return a
	}
	if F32_lt(a, b) {
		return b
	}
	return a
}

// minMaxNaN32 applies the shared NaN rule and reports whether it decided the result.
func minMaxNaN32(a, b Float32_t) (Float32_t, bool) {
	uiA, uiB := uint32(a), uint32(b)
	// Signaling NaN is the only invalid operation here; a quiet NaN passes silently.
	if Softfloat_isSigNaNF32UI(uiA) || Softfloat_isSigNaNF32UI(uiB) {
		Softfloat_raiseFlags(Softfloat_flag_invalid)
	}
	aNaN, bNaN := IsNaNF32UI(uiA), IsNaNF32UI(uiB)
	switch {
	case aNaN && bNaN:
		return Float32_t(0x7FC00000), true
	case aNaN:
		return b, true
	case bNaN:
		return a, true
	}
	return 0, false
}

func bothZeroF32(a, b Float32_t) bool {
	return uint32(a)&0x7FFFFFFF == 0 && uint32(b)&0x7FFFFFFF == 0
}
