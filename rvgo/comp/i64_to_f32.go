package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func I64_to_f32(a int64) Float32_t {
	var sign bool
	var absA uint64
	var shiftDist int8
	var ui uint32
	var sig uint32

	sign = (a < 0)
	if sign {
		absA = -uint64(a)
	} else {
		absA = uint64(a)
	}
	shiftDist = int8(Softfloat_countLeadingZeros64(absA) - 40)
	if shiftDist >= 0 {
		if a != 0 {
			ui = PackToF32UI(sign, uint32(0x95-int16(shiftDist)), uint32(absA)<<uint32(shiftDist))
		} else {
			ui = 0
		}
		return Float32_t(ui)
	} else {
		shiftDist += 7
		if shiftDist < 0 {
			sig = uint32(Softfloat_shortShiftRightJam64(absA, uint8(-shiftDist)))
		} else {
			sig = uint32(absA) << uint32(shiftDist)
		}
		return Softfloat_roundPackToF32(sign, int16(0x9C-int16(shiftDist)), uint32(sig))
	}
}
