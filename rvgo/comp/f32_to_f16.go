package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F32_to_f16(a Float32_t) Float16_t {
	uiA := uint32(a)
	var sign bool
	var exp int16
	var frac uint32
	var uiZ, frac16 uint16

	sign = SignF32UI(uiA)
	exp = ExpF32UI(uiA)
	frac = FracF32UI(uiA)

	if exp == 0xFF {
		if frac != 0 {
			uiZ = 0x7E00
		} else {
			uiZ = PackToF16UI(sign, 0x1F, 0)
		}
		// uiZ
		return Float16_t(uiZ)
	}

	frac16 = uint16(frac>>9 | uint32(BoolToInt((frac&0x1FF) != 0)))
	if (uint16(exp) | frac16) == 0 {
		uiZ = PackToF16UI(sign, 0, 0)
		// uiZ
		return Float16_t(uiZ)
	}
	return Softfloat_roundPackToF16(sign, exp-0x71, frac16|0x4000)
}
