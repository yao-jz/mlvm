package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F64_lt_quiet(a, b Float64_t) bool {
	uiA := uint64(a)
	uiB := uint64(b)
	var signA, signB bool

	if IsNaNF64UI(uiA) || IsNaNF64UI(uiB) {
		return false
	}
	signA = SignF64UI(uiA)
	signB = SignF64UI(uiB)
	if signA != signB {
		return signA && ((uiA|uiB)&0x7FFFFFFFFFFFFFFF) != 0
	} else {
		return (uiA != uiB) && (signA != (uiA < uiB))
	}
}
