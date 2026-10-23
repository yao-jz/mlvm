package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func I32_to_f32(a int32) Float32_t {
	var sign bool
	var uiZ uint32
	var absA uint32

	sign = (a < 0)
	if (a & 0x7FFFFFFF) == 0 {
		if sign {
			uiZ = PackToF32UI(true, 0x9E, 0)
		} else {
			uiZ = 0
		}
		return Float32_t(uiZ)
	}
	if sign {
		absA = -uint32(a)
	} else {
		absA = uint32(a)
	}
	return Softfloat_normRoundPackToF32(sign, 0x9C, absA)
}
