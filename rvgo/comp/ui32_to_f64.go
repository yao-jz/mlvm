package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func UI32ToF64(a uint32) Float64_t {
	var uiZ uint64
	var shiftDist int8
	var ui uint64

	if a == 0 {
		uiZ = 0
	} else {
		shiftDist = int8(Softfloat_countLeadingZeros32(a) + 21)
		uiZ = PackToF64UI(false, 0x432-uint64(shiftDist), uint64(a)<<uint64(shiftDist))
	}
	ui = uiZ
	return Float64_t(ui)
}
