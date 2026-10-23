package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

// Softfloat_roundMToI64 rounds a three-word significand to a signed 64-bit integer.
//
// This is the path FCVT.L.D actually takes - not Softfloat_roundToI64 - so it is where
// FCVT.L.D's inexact and invalid flags come from.
func Softfloat_roundMToI64(sign bool, extSigPtr []uint32, exact bool) int64 {
	roundingMode := Softfloat_roundingMode

	sig := uint64(extSigPtr[IndexWord(3, 2)])<<32 | uint64(extSigPtr[IndexWord(3, 1)])
	sigExtra := extSigPtr[IndexWordLo(3)]

	increment := false
	if roundingMode == Softfloat_round_near_maxMag || roundingMode == Softfloat_round_near_even {
		increment = sigExtra >= 0x80000000
	} else if sign {
		if sig == 0 && sigExtra == 0 {
			return 0
		}
		increment = roundingMode == Softfloat_round_min
	} else {
		increment = roundingMode == Softfloat_round_max && sigExtra != 0
	}

	if increment {
		sig++
		if sig == 0 {
			return i64Invalid(sign)
		}
		if sigExtra == 0x80000000 && roundingMode == Softfloat_round_near_even {
			sig &= ^uint64(1)
		}
	}

	var uiZ uint64
	if sign {
		uiZ = -sig
	} else {
		uiZ = sig
	}
	z := int64(uiZ)
	if z != 0 && (z < 0) != sign {
		return i64Invalid(sign)
	}
	if exact && sigExtra != 0 {
		Softfloat_raiseFlags(Softfloat_flag_inexact)
	}
	return z
}
