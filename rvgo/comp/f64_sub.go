package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F64_sub(a, b Float64_t) Float64_t {
	uiA := uint64(a)
	uiB := uint64(b)
	var signA, signB bool

	signA = SignF64UI(uiA)
	signB = SignF64UI(uiB)

	if signA == signB {
		return Softfloat_subMagsF64(uiA, uiB, signA)
	} else {
		return Softfloat_addMagsF64(uiA, uiB, signA)
	}
}
