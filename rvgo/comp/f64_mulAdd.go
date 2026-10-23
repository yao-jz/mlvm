package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F64_mulAdd(a, b, c Float64_t) Float64_t {
	uiA := uint64(a)
	uiB := uint64(b)
	uiC := uint64(c)
	return Softfloat_mulAddF64(uiA, uiB, uiC, 0)
}
