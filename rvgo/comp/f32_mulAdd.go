package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F32_mulAdd(a, b, c Float32_t) Float32_t {
	uiA := uint32(a)
	uiB := uint32(b)
	uiC := uint32(c)
	return Softfloat_mulAddF32(uiA, uiB, uiC, 0)
}
