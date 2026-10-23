package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F32_le_quiet(a Float32_t, b Float32_t) bool {
	uiA := uint32(a)
	uiB := uint32(b)
	var signA, signB bool

	if IsNaNF32UI(uiA) || IsNaNF32UI(uiB) {
		return false
	}
	signA = SignF32UI(uiA)
	signB = SignF32UI(uiB)
	if signA != signB {
		return signA || uint32((uiA|uiB)<<1) == 0
	} else {
		return (uiA == uiB) || (signA != (uiA < uiB))
	}
}
