package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F64_to_f16(a Float64_t) Float16_t {
	uiA := uint64(a)
	var sign bool
	var exp int16
	var frac uint64
	var uiZ uint16
	var frac16 uint16

	sign = SignF64UI(uiA)
	exp = ExpF64UI(uiA)
	frac = FracF64UI(uiA)

	if exp == 0x7FF {
		if frac != 0 {
			uiZ = 0x7E00
		} else {
			uiZ = PackToF16UI(sign, 0x1F, 0)
		}
		// uiZ
		return Float16_t(uiZ)
	}

	frac16 = uint16(Softfloat_shortShiftRightJam64(frac, 38))
	if (uint16(exp) | frac16) == 0 {
		uiZ = PackToF16UI(sign, 0, 0)
		// uiZ
		return Float16_t(uiZ)
	}
	return Softfloat_roundPackToF16(sign, exp-0x3F1, frac16|0x4000)
}
