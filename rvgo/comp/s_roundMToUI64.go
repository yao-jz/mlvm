package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

// Softfloat_roundMToUI64 is Softfloat_roundMToI64's unsigned twin, and the path FCVT.LU.D
// takes.
func Softfloat_roundMToUI64(sign bool, extSigPtr []uint32, exact bool) uint64 {
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
		if roundingMode == Softfloat_round_min {
			return ui64Invalid(sign)
		}
	} else {
		increment = roundingMode == Softfloat_round_max && sigExtra != 0
	}

	if increment {
		sig++
		if sig == 0 {
			return ui64Invalid(sign)
		}
		if sigExtra == 0x80000000 && roundingMode == Softfloat_round_near_even {
			sig &= ^uint64(1)
		}
	}

	if sign && sig != 0 {
		return ui64Invalid(sign)
	}
	if exact && sigExtra != 0 {
		Softfloat_raiseFlags(Softfloat_flag_inexact)
	}
	return sig
}
