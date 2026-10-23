package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F16_to_ui64(a Float16_t, exact bool) uint64 {
	var uiA uint16
	var sign bool
	var exp int8
	var frac uint16
	var sig32 uint32
	var shiftDist int8

	var extSig [3]uint32

	uiA = uint16(a)
	sign = SignF16UI(uiA)
	exp = ExpF16UI(uiA)
	frac = FracF16UI(uiA)

	if exp == 0x1F {
		if frac != 0 {
			return 0xFFFFFFFFFFFFFFFF
		} else {
			if sign {
				return 0
			} else {
				return 0xFFFFFFFFFFFFFFFF
			}
		}
	}

	sig32 = uint32(frac)
	if exp != 0 {
		sig32 |= 0x0400
		shiftDist = exp - 0x19
		if (shiftDist >= 0) && !sign {
			return uint64(sig32 << shiftDist)
		}
		shiftDist = exp - 0x0D
		if shiftDist > 0 {
			sig32 <<= shiftDist
		}
	}
	extSig[IndexWord(3, 2)] = 0
	extSig[IndexWord(3, 1)] = sig32 >> 12
	extSig[IndexWord(3, 0)] = sig32 << 20
	return Softfloat_roundMToUI64(sign, extSig[:], exact)
}
