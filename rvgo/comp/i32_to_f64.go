package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func I32_to_f64(a int32) Float64_t {
	var uiZ uint64
	var sign bool
	var absA uint32
	var shiftDist int8

	if a == 0 {
		uiZ = 0
	} else {
		sign = (a < 0)
		if sign {
			absA = -uint32(a)
		} else {
			absA = uint32(a)
		}
		shiftDist = int8(Softfloat_countLeadingZeros32(absA) + 21)
		uiZ = PackToF64UI(sign, uint64(0x432-int16(shiftDist)), uint64(absA)<<uint64(shiftDist))
	}
	return Float64_t(uiZ)
}
