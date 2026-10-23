package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func UI32ToF16(a uint32) Float16_t {
	var shiftDist int8
	var ui uint16
	var sig uint16

	shiftDist = int8(Softfloat_countLeadingZeros32(a) - 21)
	if shiftDist >= 0 {
		if a != 0 {
			ui = PackToF16UI(false, uint16(0x18-shiftDist), uint16(a<<shiftDist))
		} else {
			ui = 0
		}
		return Float16_t(ui)
	} else {
		shiftDist += 4
		if shiftDist < 0 {
			temp := uint32(0)
			if uint32(a<<uint(shiftDist&31)) != 0 {
				temp = 1
			}
			sig = uint16(a>>uint(-shiftDist)) | uint16(temp)
		} else {
			sig = uint16(a << uint(shiftDist))
		}
		return Softfloat_roundPackToF16(false, int16(0x1C-shiftDist), sig)
	}
}
