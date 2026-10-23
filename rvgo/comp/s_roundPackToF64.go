package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

// Softfloat_roundPackToF64 rounds a wide intermediate significand into a double.
//
// Every double-precision arithmetic result goes through here, so this is where `inexact`,
// `underflow` and `overflow` are raised, and where the rounding mode takes effect. The
// original port hardcoded round-to-nearest-even and dropped all three flags, which is why
// `fsflags` always read zero.
func Softfloat_roundPackToF64(sign bool, exp int16, sig uint64) Float64_t {
	roundingMode := Softfloat_roundingMode
	roundNearEven := roundingMode == Softfloat_round_near_even

	roundIncrement := uint16(0x200)
	if !roundNearEven && roundingMode != Softfloat_round_near_maxMag {
		// Round away from zero only when the mode points that way for this sign; otherwise
		// truncate.
		awayFromZero := Softfloat_round_max
		if sign {
			awayFromZero = Softfloat_round_min
		}
		if roundingMode == awayFromZero {
			roundIncrement = 0x3FF
		} else {
			roundIncrement = 0
		}
	}
	roundBits := uint16(sig) & 0x3FF

	if 0x7FD <= uint16(exp) {
		if exp < 0 {
			// Tiny. Whether that counts as underflow depends on the tininess-detection mode
			// and on whether rounding lifted the value back into the normal range.
			isTiny := Softfloat_detectTininess == SoftfloatTininessBeforeRounding ||
				exp < -1 || sig+uint64(roundIncrement) < 0x8000000000000000
			sig = Softfloat_shiftRightJam64(sig, uint32(-exp))
			exp = 0
			roundBits = uint16(sig) & 0x3FF
			if isTiny && roundBits != 0 {
				Softfloat_raiseFlags(Softfloat_flag_underflow)
			}
		} else if (0x7FD < exp) || (0x8000000000000000 <= sig+uint64(roundIncrement)) {
			Softfloat_raiseFlags(Softfloat_flag_overflow | Softfloat_flag_inexact)
			uiZ := PackToF64UI(sign, 0x7FF, 0)
			if roundIncrement == 0 {
				uiZ -= 1
			}
			return Float64_t(uiZ)
		}
	}

	sig = (sig + uint64(roundIncrement)) >> 10
	if roundBits != 0 {
		Softfloat_raiseFlags(Softfloat_flag_inexact)
	}
	// Ties to even: clear the low bit when the discarded part was exactly half.
	if roundBits^0x200 == 0 && roundNearEven {
		sig &= ^uint64(1)
	}
	if sig == 0 {
		exp = 0
	}
	return Float64_t(PackToF64UI(sign, uint64(exp), sig))
}
