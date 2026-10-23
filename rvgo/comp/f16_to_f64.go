package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F16_to_f64(a Float16_t) Float64_t {
	var uiA uint16
	var sign bool
	var exp int8
	var frac uint16
	var uiZ uint64
	var exp1 int8
	var frac1 uint16

	uiA = uint16(a)
	sign = SignF16UI(uiA)
	exp = ExpF16UI(uiA)
	frac = FracF16UI(uiA)

	if exp == 0x1F {
		if frac != 0 {
			uiZ = uint64(0x7FF8000000000000)
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
		exp1, frac1 = Softfloat_normSubnormalF16Sig(frac)
		exp = exp1 - 1
		frac = frac1
	}
	uiZ = PackToF64UI(sign, uint64(uint32(exp)+0x3F0), uint64(frac)<<42)
	// uiZ
	return Float64_t(uiZ)
}
