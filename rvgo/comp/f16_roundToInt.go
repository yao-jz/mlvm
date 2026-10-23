package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F16_roundToInt(a Float16_t, exact bool) Float16_t {
	var uiA, uiZ uint16
	var exp int8
	var lastBitMask, roundBitsMask uint16

	uiA = uint16(a)
	exp = ExpF16UI(uiA)
	if exp <= 0xE {
		if uint16(uiA<<1) == 0 {
			return a
		}
		uiZ = uiA & PackToF16UI(true, 0, 0)
		if FracF16UI(uiA) == 0 {

		} else {
			if exp == 0xE {
				uiZ |= PackToF16UI(false, 0xF, 0)
			}
		}
		// uiZ
		return Float16_t(uiZ)
	}

	if 0x19 <= exp {
		if (exp == 0x1F) && FracF16UI(uiA) != 0 {
			uiZ = Softfloat_propagateNaNF16UI(uiA, 0)
			// uiZ
			return Float16_t(uiZ)
		}
		return a
	}

	uiZ = uiA
	lastBitMask = uint16(1) << (0x19 - exp)
	roundBitsMask = lastBitMask - 1
	uiZ += lastBitMask >> 1
	if (uiZ & roundBitsMask) == 0 {
		uiZ &= ^lastBitMask
	}
	uiZ &= ^roundBitsMask
	// uiZ
	return Float16_t(uiZ)
}
