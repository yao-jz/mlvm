package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F32_to_ui32(a Float32_t, exact bool) uint32 {
	uiA := uint32(a)
	var sign bool
	var exp int16
	var sig uint32
	var sig64 uint64
	var shiftDist int16

	sign = SignF32UI(uiA)
	exp = ExpF32UI(uiA)
	sig = FracF32UI(uiA)

	if exp == 0xFF && sig != 0 {
		sign = false
	}
	if exp != 0 {
		sig |= 0x00800000
	}
	sig64 = uint64(sig) << 32
	shiftDist = 0xAA - exp
	if 0 < shiftDist {
		sig64 = Softfloat_shiftRightJam64(sig64, uint32(shiftDist))
	}
	return Softfloat_roundToUI32(sign, sig64, exact)
}
