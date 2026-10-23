package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F16_eq_signaling(a Float16_t, b Float16_t) bool {
	var uiA, uiB uint16

	uiA = uint16(a)
	uiB = uint16(b)
	if IsNaNF16UI(uiA) || IsNaNF16UI(uiB) {
		return false
	}
	return (uiA == uiB) || uint16((uiA|uiB)<<1) == 0
}
