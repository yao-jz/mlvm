package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F64_to_ui64(a Float64_t, exact bool) uint64 {
	var uiA uint64
	var sign bool
	var exp int16
	var sig uint64
	var shiftDist int16

	var extSig [3]uint32

	// The operand was never read: uiA was declared and then used, so every conversion
	// decomposed the constant zero and the function returned 0 for every input. Nothing
	// caught it because no conformance suite exercises FCVT.LU.D and the Go compiler treats
	// a declared-and-used variable as used.
	uiA = uint64(a)

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
				return 0xFFFFFFFFFFFFFFFF
			} else {
				if sign {
					return 0
				} else {
					return 0xFFFFFFFFFFFFFFFF
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
	return Softfloat_roundMToUI64(sign, extSig[:], exact)
}
