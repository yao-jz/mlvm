package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func UI64ToF32(a uint64) Float32_t {
	var shiftDist int8
	var u uint32
	var sig uint32

	shiftDist = int8(Softfloat_countLeadingZeros64(a) - 40)
	if 0 <= shiftDist {
		if a != 0 {
			u = PackToF32UI(false, 0x95-uint32(shiftDist), uint32(uint32(a)<<uint64(shiftDist)))
		} else {
			u = 0
		}
		return Float32_t(u)
	} else {
		shiftDist += 7
		if shiftDist < 0 {
			sig = uint32(Softfloat_shortShiftRightJam64(a, uint8(-shiftDist)))
		} else {
			sig = uint32(uint32(a) << uint64(shiftDist))
		}
		return Softfloat_roundPackToF32(false, int16(0x9C-int16(shiftDist)), sig)
	}
}
