package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F16_lt(a, b Float16_t) bool {
	var uiA, uiB uint16
	var signA, signB bool

	uiA = uint16(a)
	uiB = uint16(b)
	if IsNaNF16UI(uiA) || IsNaNF16UI(uiB) {
		return false
	}
	signA = SignF16UI(uiA)
	signB = SignF16UI(uiB)
	if signA != signB {
		return signA && (uint16((uiA|uiB)<<1) != 0)
	} else {
		return (uiA != uiB) && IntToBool(BoolToInt(signA)^BoolToInt(uiA < uiB))
	}
}
