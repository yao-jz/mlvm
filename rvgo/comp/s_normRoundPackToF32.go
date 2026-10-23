package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_normRoundPackToF32(sign bool, exp int16, sig uint32) Float32_t {
	var shiftDist int8
	var ui uint32

	shiftDist = int8(Softfloat_countLeadingZeros32(sig) - 1)
	exp -= int16(shiftDist)
	if (7 <= shiftDist) && (uint32(exp) < 0xFD) {
		if sig != 0 {
			ui = PackToF32UI(sign, uint32(exp), sig<<(shiftDist-7))
		} else {
			ui = PackToF32UI(sign, 0, sig<<(shiftDist-7))
		}
		return Float32_t(ui)
	} else {
		return Softfloat_roundPackToF32(sign, exp, sig<<shiftDist)
	}
}
