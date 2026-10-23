package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F16_to_ui32(a Float16_t, exact bool) uint32 {
	var uiA uint16
	var sign bool
	var exp int8
	var frac uint16
	var sig32 uint32
	var shiftDist int8

	uiA = uint16(a)
	sign = SignF16UI(uiA)
	exp = ExpF16UI(uiA)
	frac = FracF16UI(uiA)

	if exp == 0x1F {
		if frac != 0 {
			return 0xFFFFFFFF
		} else {
			if sign {
				return 0
			} else {
				return 0xFFFFFFFF
			}
		}
	}

	sig32 = uint32(frac)
	if exp != 0 {
		sig32 |= 0x0400
		shiftDist = exp - 0x19
		if (shiftDist >= 0) && !sign {
			return sig32 << shiftDist
		}
		shiftDist = exp - 0x0D
		if shiftDist > 0 {
			sig32 <<= shiftDist
		}
	}
	return Softfloat_roundToUI32(sign, uint64(sig32), exact)
}
