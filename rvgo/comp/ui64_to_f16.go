package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func UI64ToF16(a uint64) Float16_t {
	var shiftDist int8
	var ui uint16
	var sig uint16

	shiftDist = int8(Softfloat_countLeadingZeros64(a) - 53)
	if 0 <= shiftDist {
		if a != 0 {
			ui = PackToF16UI(false, 0x18-uint16(shiftDist), uint16(uint16(a)<<uint64(shiftDist)))
		} else {
			ui = 0
		}
		return Float16_t(ui)
	} else {
		shiftDist += 4
		if shiftDist < 0 {
			sig = uint16(Softfloat_shortShiftRightJam64(a, uint8(-shiftDist)))
		} else {
			sig = uint16(uint16(a) << uint64(shiftDist))
		}
		return Softfloat_roundPackToF16(false, int16(0x1C-shiftDist), sig)
	}
}
