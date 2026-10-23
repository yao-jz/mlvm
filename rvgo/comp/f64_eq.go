package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F64_eq(a, b Float64_t) bool {
	uiA := uint64(a)
	uiB := uint64(b)
	if IsNaNF64UI(uiA) || IsNaNF64UI(uiB) {
		// Quiet comparison: only a signaling NaN is an invalid operation.
		if Softfloat_isSigNaNF64UI(uiA) || Softfloat_isSigNaNF64UI(uiB) {
			Softfloat_raiseFlags(Softfloat_flag_invalid)
		}
		return false
	}
	return (uiA == uiB) || ((uiA|uiB)&0x7FFFFFFFFFFFFFFF) == 0
}
