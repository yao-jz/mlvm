package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

// Softfloat_roundToI32 rounds a shifted significand to a signed 32-bit integer.
//
// `exact` asks for the inexact flag when the value had a fractional part - which is what
// FCVT.W.D wants, and what the original port dropped along with the rounding modes.
func Softfloat_roundToI32(sign bool, sig uint64, exact bool) int32 {
	roundingMode := Softfloat_roundingMode

	roundIncrement := uint16(0x800)
	if roundingMode != Softfloat_round_near_maxMag && roundingMode != Softfloat_round_near_even {
		roundIncrement = 0
		awayFromZero := Softfloat_round_max
		if sign {
			awayFromZero = Softfloat_round_min
		}
		if roundingMode == awayFromZero {
			roundIncrement = 0xFFF
		}
	}

	roundBits := uint16(sig & 0xFFF)
	sig += uint64(roundIncrement)
	if sig&0xFFFFF00000000000 != 0 {
		return i32Invalid(sign)
	}

	sig32 := uint32(sig >> 12)
	if roundBits == 0x800 && roundingMode == Softfloat_round_near_even {
		sig32 &= ^uint32(1)
	}
	var ui uint32
	if sign {
		ui = uint32(-int32(sig32))
	} else {
		ui = sig32
	}
	z := int32(ui)
	if z != 0 && ((z < 0) != sign) {
		return i32Invalid(sign)
	}
	if exact && roundBits != 0 {
		Softfloat_raiseFlags(Softfloat_flag_inexact)
	}
	return z
}

// i32Invalid is the out-of-range result: RISC-V wants the saturated value *and* the invalid
// flag, and the flag is what distinguishes a genuine INT32_MAX from a clamped one.
func i32Invalid(sign bool) int32 {
	Softfloat_raiseFlags(Softfloat_flag_invalid)
	if sign {
		return -0x7FFFFFFF - 1
	}
	return 0x7FFFFFFF
}
