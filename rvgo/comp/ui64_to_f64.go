package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func UI64ToF64(a uint64) Float64_t {
	var ui uint64
	if a == 0 {
		ui = 0
		return Float64_t(ui)
	}
	if (a & uint64(0x8000000000000000)) != 0 {
		return Softfloat_roundPackToF64(false, 0x43D, Softfloat_shortShiftRightJam64(a, 1))
	} else {
		return Softfloat_normRoundPackToF64(false, 0x43C, a)
	}
}
