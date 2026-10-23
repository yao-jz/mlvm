package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func I64_to_f64(a int64) Float64_t {
	var sign bool
	var uiZ uint64
	var absA uint64

	sign = (a < 0)
	if (a & 0x7FFFFFFFFFFFFFFF) == 0 {
		if sign {
			uiZ = PackToF64UI(true, 0x43E, 0)
		} else {
			uiZ = 0
		}
		return Float64_t(uiZ)
	}
	if sign {
		absA = -uint64(a)
	} else {
		absA = uint64(a)
	}
	return Softfloat_normRoundPackToF64(sign, 0x43C, absA)
}
