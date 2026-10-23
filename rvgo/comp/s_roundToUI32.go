package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

// Softfloat_roundToUI32 is Softfloat_roundToI32's unsigned twin.
func Softfloat_roundToUI32(sign bool, sig uint64, exact bool) uint32 {
	roundingMode := Softfloat_roundingMode

	roundIncrement := uint16(0x800)
	if roundingMode != Softfloat_round_near_maxMag && roundingMode != Softfloat_round_near_even {
		roundIncrement = 0
		if sign {
			if sig != 0 && roundingMode == Softfloat_round_min {
				return ui32Invalid(sign)
			}
		} else if roundingMode == Softfloat_round_max {
			roundIncrement = 0xFFF
		}
	}

	roundBits := uint16(sig & 0xFFF)
	sig += uint64(roundIncrement)
	if sig&0xFFFFF00000000000 != 0 {
		return ui32Invalid(sign)
	}

	z := uint32(sig >> 12)
	if roundBits == 0x800 && roundingMode == Softfloat_round_near_even {
		z &= ^uint32(1)
	}
	if sign && z != 0 {
		return ui32Invalid(sign)
	}
	if exact && roundBits != 0 {
		Softfloat_raiseFlags(Softfloat_flag_inexact)
	}
	return z
}

func ui32Invalid(sign bool) uint32 {
	Softfloat_raiseFlags(Softfloat_flag_invalid)
	if sign {
		return 0
	}
	return 0xFFFFFFFF
}
