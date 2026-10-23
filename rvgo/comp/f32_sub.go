package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F32_sub(a, b Float32_t) Float32_t {
	uiA := uint32(a)
	uiB := uint32(b)
	if SignF32UI(uiA ^ uiB) {
		return Softfloat_addMagsF32(uiA, uiB)
	} else {
		return Softfloat_subMagsF32(uiA, uiB)
	}
}
