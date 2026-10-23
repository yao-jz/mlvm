package riscv

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

// Softfloat_propagateNaNF64UI returns the result of an operation with a NaN operand.
//
// RISC-V always returns the canonical NaN rather than propagating a payload, which is what
// makes MLVM's float results reproducible across hosts. But a *signaling* NaN operand is an
// invalid operation, and the flag has to be raised even though the value does not change.
func Softfloat_propagateNaNF64UI(uiA, uiB uint64) uint64 {
	if Softfloat_isSigNaNF64UI(uiA) || Softfloat_isSigNaNF64UI(uiB) {
		Softfloat_raiseFlags(Softfloat_flag_invalid)
	}
	return 0x7FF8000000000000
}
