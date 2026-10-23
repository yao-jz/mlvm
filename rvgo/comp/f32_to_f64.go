package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F32_to_f64(a Float32_t) Float64_t {
	uiA := uint32(a)
	var sign bool
	var exp int16
	var frac uint32
	var uiZ uint64

	sign = SignF32UI(uiA)
	exp = ExpF32UI(uiA)
	frac = FracF32UI(uiA)

	if exp == 0xFF {
		if frac != 0 {
			uiZ = 0x7FF8000000000000
		} else {
			uiZ = PackToF64UI(sign, 0x7FF, 0)
		}
		// uiZ
		return Float64_t(uiZ)
	}

	if exp == 0 {
		if frac == 0 {
			uiZ = PackToF64UI(sign, 0, 0)
			// uiZ
			return Float64_t(uiZ)
		}
		exp1, frac1 := Softfloat_normSubnormalF32Sig(frac)
		exp = exp1 - 1
		frac = frac1
	}
	uiZ = PackToF64UI(sign, uint64(exp+0x380), uint64(frac)<<29)
	// uiZ
	return Float64_t(uiZ)
}
