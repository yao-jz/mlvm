package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func I32_to_f16(a int32) Float16_t {
	var sign bool
	var absA uint32
	var shiftDist int8
	var ui uint16
	var sig uint16

	sign = (a < 0)
	if sign {
		absA = -uint32(a)
	} else {
		absA = uint32(a)
	}
	shiftDist = int8(Softfloat_countLeadingZeros32(absA) - 21)
	if shiftDist >= 0 {
		if a != 0 {
			ui = PackToF16UI(sign, uint16(0x18-shiftDist), uint16(absA)<<uint16(shiftDist))
		} else {
			ui = 0
		}
		return Float16_t(ui)
	} else {
		shiftDist += 4
		if shiftDist < 0 {
			sig = uint16(absA>>uint32(-shiftDist) | uint32(BoolToInt(uint32(absA<<uint32(shiftDist&31)) != 0)))
		} else {
			sig = uint16(absA) << uint16(shiftDist)
		}
		return Softfloat_roundPackToF16(sign, int16(0x1C-shiftDist), uint16(sig))
	}
}
