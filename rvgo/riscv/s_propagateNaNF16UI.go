package riscv

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

// Softfloat_propagateNaNF16UI returns the result of an operation with a NaN operand.
//
// RISC-V always returns the canonical NaN rather than propagating a payload, which is what
// makes MLVM's float results reproducible across hosts. But a *signaling* NaN operand is an
// invalid operation, and the flag has to be raised even though the value does not change.
func Softfloat_propagateNaNF16UI(uiA, uiB uint16) uint16 {
	if Softfloat_isSigNaNF16UI(uiA) || Softfloat_isSigNaNF16UI(uiB) {
		Softfloat_raiseFlags(Softfloat_flag_invalid)
	}
	return 0x7E00
}
