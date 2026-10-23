package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F16_mulAdd(a, b, c Float16_t) Float16_t {
	var uiA, uiB, uiC uint16

	uiA = uint16(a)
	uiB = uint16(b)
	uiC = uint16(c)
	return Softfloat_mulAddF16(uiA, uiB, uiC, 0)
}
