package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F64_to_f32(a Float64_t) Float32_t {
	uiA := uint64(a)
	var sign bool
	var exp int16
	var frac uint64
	var uiZ, frac32 uint32

	sign = SignF64UI(uiA)
	exp = ExpF64UI(uiA)
	frac = FracF64UI(uiA)

	if exp == 0x7FF {
		if frac != 0 {
			uiZ = 0x7FC00000
		} else {
			uiZ = PackToF32UI(sign, 0xFF, 0)
		}
		// uiZ
		return Float32_t(uiZ)
	}

	frac32 = uint32(Softfloat_shortShiftRightJam64(frac, 22))
	if (uint32(exp) | frac32) == 0 {
		uiZ = PackToF32UI(sign, 0, 0)
		// uiZ
		return Float32_t(uiZ)
	}
	return Softfloat_roundPackToF32(sign, exp-0x381, frac32|0x40000000)
}
