package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F16_isSignalingNaN(a Float16_t) bool {
	uiA := uint16(a)
	return Softfloat_isSigNaNF16UI(uiA)
}
