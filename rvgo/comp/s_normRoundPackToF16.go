package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_normRoundPackToF16(sign bool, exp int16, sig uint16) Float16_t {
	var shiftDist int8
	var ui uint16

	shiftDist = int8(Softfloat_countLeadingZeros16(sig) - 1)
	exp -= int16(shiftDist)
	if (4 <= shiftDist) && (uint32(exp) < 0x1D) {
		if sig != 0 {
			ui = PackToF16UI(sign, uint16(exp), sig<<(shiftDist-4))
		} else {
			ui = PackToF16UI(sign, 0, sig<<(shiftDist-4))
		}
		return Float16_t(ui)
	} else {
		return Softfloat_roundPackToF16(sign, exp, sig<<shiftDist)
	}
}
