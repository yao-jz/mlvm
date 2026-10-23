package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

// Softfloat_roundPackToF32 is Softfloat_roundPackToF64's single-precision twin; see there for
// why the flags and the rounding mode matter.
func Softfloat_roundPackToF32(sign bool, exp int16, sig uint32) Float32_t {
	roundingMode := Softfloat_roundingMode
	roundNearEven := roundingMode == Softfloat_round_near_even

	roundIncrement := uint16(0x40)
	if !roundNearEven && roundingMode != Softfloat_round_near_maxMag {
		awayFromZero := Softfloat_round_max
		if sign {
			awayFromZero = Softfloat_round_min
		}
		if roundingMode == awayFromZero {
			roundIncrement = 0x7F
		} else {
			roundIncrement = 0
		}
	}
	roundBits := uint16(sig) & 0x7F

	if 0xFD <= uint32(exp) {
		if exp < 0 {
			isTiny := Softfloat_detectTininess == SoftfloatTininessBeforeRounding ||
				exp < -1 || sig+uint32(roundIncrement) < 0x80000000
			sig = Softfloat_shiftRightJam32(sig, uint16(-exp))
			exp = 0
			roundBits = uint16(sig) & 0x7F
			if isTiny && roundBits != 0 {
				Softfloat_raiseFlags(Softfloat_flag_underflow)
			}
		} else if (0xFD < exp) || (0x80000000 <= sig+uint32(roundIncrement)) {
			Softfloat_raiseFlags(Softfloat_flag_overflow | Softfloat_flag_inexact)
			uiZ := PackToF32UI(sign, 0xFF, 0)
			if roundIncrement == 0 {
				uiZ -= 1
			}
			return Float32_t(uiZ)
		}
	}

	sig = (sig + uint32(roundIncrement)) >> 7
	if roundBits != 0 {
		Softfloat_raiseFlags(Softfloat_flag_inexact)
	}
	if roundBits^0x40 == 0 && roundNearEven {
		sig &= ^uint32(1)
	}
	if sig == 0 {
		exp = 0
	}
	return Float32_t(PackToF32UI(sign, uint32(exp), sig))
}
