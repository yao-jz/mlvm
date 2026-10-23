package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F64_to_i32(a Float64_t, exact bool) int32 {
	uiA := uint64(a)
	var sign bool
	var exp int16
	var sig uint64
	var shiftDist int16

	sign = SignF64UI(uiA)
	exp = ExpF64UI(uiA)
	sig = FracF64UI(uiA)

	if (exp == 0x7FF) && (sig != 0) {
		sign = false
	}
	if exp != 0 {
		sig |= 0x0010000000000000
	}
	shiftDist = 0x427 - exp
	if shiftDist > 0 {
		sig = Softfloat_shiftRightJam64(sig, uint32(shiftDist))
	}
	return Softfloat_roundToI32(sign, sig, exact)
}
