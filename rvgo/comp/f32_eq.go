package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F32_eq(a, b Float32_t) bool {
	uiA := uint32(a)
	uiB := uint32(b)

	if IsNaNF32UI(uiA) || IsNaNF32UI(uiB) {
		// Quiet comparison: only a signaling NaN is an invalid operation.
		if Softfloat_isSigNaNF32UI(uiA) || Softfloat_isSigNaNF32UI(uiB) {
			Softfloat_raiseFlags(Softfloat_flag_invalid)
		}
		return false
	}
	return (uiA == uiB) || uint32((uiA|uiB)<<1) == 0
}
