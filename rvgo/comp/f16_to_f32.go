package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F16_to_f32(a Float16_t) Float32_t {
	var uiA uint16
	var sign bool
	var exp int8
	var frac uint16
	var uiZ uint32
	var exp1 int8
	var frac1 uint16

	uiA = uint16(a)
	sign = SignF16UI(uiA)
	exp = ExpF16UI(uiA)
	frac = FracF16UI(uiA)

	if exp == 0x1F {
		if frac != 0 {
			uiZ = uint32(0x7FC00000)
		} else {
			uiZ = PackToF32UI(sign, 0xFF, 0)
		}
		// uiZ
		return Float32_t(uiZ)
	}
	if exp == 0 {
		if frac == 0 {
			uiZ = PackToF32UI(sign, 0, 0)
			// uiZ
			return Float32_t(uiZ)
		}
		exp1, frac1 = Softfloat_normSubnormalF16Sig(frac)
		exp = exp1 - 1
		frac = frac1
	}
	uiZ = PackToF32UI(sign, uint32(exp+0x70), uint32(frac)<<13)
	// uiZ
	return Float32_t(uiZ)
}
