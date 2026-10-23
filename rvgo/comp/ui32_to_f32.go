package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func UI32ToF32(a uint32) Float32_t {
	var ui uint32
	if a == 0 {
		ui = 0
		return Float32_t(ui)
	}
	if (a & 0x80000000) != 0 {
		return Softfloat_roundPackToF32(false, 0x9D, a>>1|(a&1))
	} else {
		return Softfloat_normRoundPackToF32(false, 0x9C, a)
	}

}
