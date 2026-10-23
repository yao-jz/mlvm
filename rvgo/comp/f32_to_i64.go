package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F32_to_i64(a Float32_t, exact bool) int64 {
	uiA := uint32(a)
	var sign bool
	var exp int16
	var sig uint32
	var shiftDist int16
	var extSig [3]uint32

	sign = SignF32UI(uiA)
	exp = ExpF32UI(uiA)
	sig = FracF32UI(uiA)

	shiftDist = 0xBE - exp
	if shiftDist < 0 {
		if exp == 0xFF && sig != 0 {
			return 0x7FFFFFFFFFFFFFFF
		} else {
			if sign {
				return -0x7FFFFFFFFFFFFFFF - 1
			} else {
				return 0x7FFFFFFFFFFFFFFF
			}
		}
	}

	if exp != 0 {
		sig |= 0x00800000
	}
	extSig[IndexWord(3, 2)] = sig << 8
	extSig[IndexWord(3, 1)] = 0
	extSig[IndexWord(3, 0)] = 0
	if shiftDist != 0 {
		Softfloat_shiftRightJamM(3, extSig[:], uint32(shiftDist), extSig[:])
	}
	return Softfloat_roundMToI64(sign, extSig[:], exact)
}
