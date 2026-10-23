package riscv

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

// Softfloat_propagateNaNF32UI returns the result of an operation with a NaN operand.
//
// RISC-V always returns the canonical NaN rather than propagating a payload, which is what
// makes MLVM's float results reproducible across hosts. But a *signaling* NaN operand is an
// invalid operation, and the flag has to be raised even though the value does not change.
func Softfloat_propagateNaNF32UI(uiA, uiB uint32) uint32 {
	if Softfloat_isSigNaNF32UI(uiA) || Softfloat_isSigNaNF32UI(uiB) {
		Softfloat_raiseFlags(Softfloat_flag_invalid)
	}
	return 0x7FC00000
}
