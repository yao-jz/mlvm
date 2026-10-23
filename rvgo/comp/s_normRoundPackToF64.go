package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_normRoundPackToF64(sign bool, exp int16, sig uint64) Float64_t {
	var shiftDist int8
	var ui uint64

	shiftDist = int8(Softfloat_countLeadingZeros64(sig) - 1)
	exp -= int16(shiftDist)
	if (10 <= shiftDist) && (uint64(exp) < 0x7FD) {
		if sig != 0 {
			ui = PackToF64UI(sign, uint64(exp), sig<<(shiftDist-10))
		} else {
			ui = PackToF64UI(sign, 0, sig<<(shiftDist-10))
		}
		return Float64_t(ui)
	} else {
		return Softfloat_roundPackToF64(sign, exp, sig<<shiftDist)
	}
}
