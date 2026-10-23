package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

// Softfloat_roundToUI64 is Softfloat_roundToI64's unsigned twin.
func Softfloat_roundToUI64(sign bool, sig uint64, sigExtra uint64, exact bool) uint64 {
	roundingMode := Softfloat_roundingMode

	increment := false
	switch {
	case roundingMode == Softfloat_round_near_maxMag:
		increment = sigExtra&0x8000000000000000 != 0
	case roundingMode == Softfloat_round_near_even:
		increment = sigExtra&0x8000000000000000 != 0 &&
			(sig&1 != 0 || sigExtra&0x7FFFFFFFFFFFFFFF != 0)
	default:
		if sign {
			// A negative value cannot round up into the unsigned range.
			if (sig != 0 || sigExtra != 0) && roundingMode == Softfloat_round_min {
				return ui64Invalid(sign)
			}
		} else {
			increment = roundingMode == Softfloat_round_max && sigExtra != 0
		}
	}

	if increment {
		sig++
		if sig == 0 {
			return ui64Invalid(sign)
		}
		if sigExtra == 0x8000000000000000 && roundingMode == Softfloat_round_near_even {
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

func ui64Invalid(sign bool) uint64 {
	Softfloat_raiseFlags(Softfloat_flag_invalid)
	if sign {
		return 0
	}
	return 0xFFFFFFFFFFFFFFFF
}
