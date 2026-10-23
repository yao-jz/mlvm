package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F64_to_i64(a Float64_t, exact bool) int64 {
	uiA := uint64(a)
	var sign bool
	var exp int16
	var sig uint64
	var shiftDist int16
	var extSig [3]uint32

	sign = SignF64UI(uiA)
	exp = ExpF64UI(uiA)
	sig = FracF64UI(uiA)

	if exp != 0 {
		sig |= 0x0010000000000000
	}
	shiftDist = 0x433 - exp
	extSig[IndexWord(3, 0)] = 0
	if shiftDist <= 0 {
		if shiftDist < -11 {
			// invalid: too large to represent at all. The saturated value alone is not
			// enough - RISC-V distinguishes a genuine INT64_MAX from a clamped one by the
			// flag, and rv64ud-p-fcvt_w test_20 checks exactly that.
			Softfloat_raiseFlags(Softfloat_flag_invalid)
			if exp == 0x7FF && FracF64UI(uiA) != 0 {
				return 0x7FFFFFFFFFFFFFFF
			} else {
				if sign {
					return -0x7FFFFFFFFFFFFFFF - 1
				} else {
					return 0x7FFFFFFFFFFFFFFF
				}
			}
		}
		sig <<= -uint64(shiftDist)
		extSig[IndexWord(3, 2)] = uint32(sig >> 32)
		extSig[IndexWord(3, 1)] = uint32(sig)
	} else {
		extSig[IndexWord(3, 2)] = uint32(sig >> 32)
		extSig[IndexWord(3, 1)] = uint32(sig)
		Softfloat_shiftRightJamM(3, extSig[:], uint32(shiftDist), extSig[:])
	}
	return Softfloat_roundMToI64(sign, extSig[:], exact)
}
