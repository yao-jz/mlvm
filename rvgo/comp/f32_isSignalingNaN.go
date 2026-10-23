package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F32_isSignalingNaN(a Float32_t) bool {
	uiA := uint32(a)
	return Softfloat_isSigNaNF32UI(uiA)
}
