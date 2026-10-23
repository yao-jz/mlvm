package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F16_sub(a, b Float16_t) Float16_t {
	var uiA, uiB uint16

	uiA = uint16(a)
	uiB = uint16(b)
	if SignF16UI(uiA ^ uiB) {
		return Softfloat_addMagsF16(uiA, uiB)
	} else {
		return Softfloat_subMagsF16(uiA, uiB)
	}
}
