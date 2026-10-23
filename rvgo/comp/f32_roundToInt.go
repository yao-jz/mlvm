package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F32_roundToInt(a Float32_t, exact bool) Float32_t {
	uiA := uint32(a)
	var exp int16
	var uiZ, lastBitMask, roundBitsMask uint32

	exp = ExpF32UI(uiA)
	if exp <= 0x7E {
		if uint32(uiA<<1) == 0 {
			return a
		}
		uiZ = uiA & PackToF32UI(true, 0, 0)
		if FracF32UI(uiA) == 0 {

		} else {
			if exp == 0x7E {
				uiZ |= PackToF32UI(false, 0x7F, 0)
			}
		}
		// uiZ
		return Float32_t(uiZ)
	}

	if exp >= 0x96 {
		if exp == 0xFF && FracF32UI(uiA) != 0 {
			uiZ = Softfloat_propagateNaNF32UI(uiA, 0)
			// uiZ
			return Float32_t(uiZ)
		}
		return a
	}
	uiZ = uiA
	lastBitMask = uint32(1) << uint32(0x96-exp)
	roundBitsMask = lastBitMask - 1
	uiZ += lastBitMask >> 1
	if uiZ&roundBitsMask == 0 {
		uiZ &= ^lastBitMask
	}
	uiZ &= ^roundBitsMask
	// uiZ
	return Float32_t(uiZ)
}
