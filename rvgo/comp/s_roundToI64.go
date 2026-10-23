package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

// Softfloat_roundToI64 rounds a 64-bit significand plus a 64-bit remainder to a signed
// 64-bit integer.
//
// The split into (sig, sigExtra) is why this cannot share code with the 32-bit form: the
// discarded part is a whole word, so "exactly half" is `sigExtra == 1<<63` rather than a
// mask on the low bits.
func Softfloat_roundToI64(sign bool, sig uint64, sigExtra uint64, exact bool) int64 {
	roundingMode := Softfloat_roundingMode

	increment := false
	switch {
	case roundingMode == Softfloat_round_near_maxMag:
		increment = sigExtra&0x8000000000000000 != 0
	case roundingMode == Softfloat_round_near_even:
		// Ties to even: at exactly half, round up only if that makes the result even.
		increment = sigExtra&0x8000000000000000 != 0 &&
			(sig&1 != 0 || sigExtra&0x7FFFFFFFFFFFFFFF != 0)
	default:
		awayFromZero := Softfloat_round_max
		if sign {
			awayFromZero = Softfloat_round_min
		}
		increment = roundingMode == awayFromZero && sigExtra != 0
	}

	if increment {
		sig++
		if sig == 0 {
			return i64Invalid(sign)
		}
		if sigExtra == 0x8000000000000000 && roundingMode == Softfloat_round_near_even {
			sig &= ^uint64(1)
		}
	}

	var ui uint64
	if sign {
		ui = uint64(-int64(sig))
	} else {
		ui = sig
	}
	z := int64(ui)
	if z != 0 && ((z < 0) != sign) {
		return i64Invalid(sign)
	}
	if exact && sigExtra != 0 {
		Softfloat_raiseFlags(Softfloat_flag_inexact)
	}
	return z
}

func i64Invalid(sign bool) int64 {
	Softfloat_raiseFlags(Softfloat_flag_invalid)
	if sign {
		return -0x7FFFFFFFFFFFFFFF - 1
	}
	return 0x7FFFFFFFFFFFFFFF
}
