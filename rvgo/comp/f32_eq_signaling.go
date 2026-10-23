package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F32_eq_signaling(a, b Float32_t) bool {
	var uiA, uiB uint32

	uiA = uint32(a)
	uiB = uint32(b)
	if IsNaNF32UI(uiA) || IsNaNF32UI(uiB) {
		return false
	}
	return (uiA == uiB) || ((uiA|uiB)<<1) == 0
}
