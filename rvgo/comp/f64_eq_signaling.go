package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F64_eq_signaling(a, b Float64_t) bool {
	uiA := uint64(a)
	uiB := uint64(b)

	if IsNaNF64UI(uiA) || IsNaNF64UI(uiB) {
		return false
	}
	return (uiA == uiB) || ((uiA|uiB)&0x7FFFFFFFFFFFFFFF) == 0
}
