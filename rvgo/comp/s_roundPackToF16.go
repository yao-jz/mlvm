package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_roundPackToF16(sign bool, exp int16, sig uint16) Float16_t {
	var roundIncrement uint16 = 0x8
	var roundBits uint16 = uint16(sig) & 0xF
	var uiZ uint16

	if 0x1D <= uint32(exp) {
		if exp < 0 {
			sig = uint16(Softfloat_shiftRightJam32(uint32(sig), uint16(-exp)))
			exp = 0
			roundBits = sig & 0xF
		} else if (0x1D < exp) || (0x8000 <= sig+roundIncrement) {
			uiZ = PackToF16UI(sign, 0x1F, 0)
			if roundIncrement == 0 {
				uiZ -= 1
			}
			return Float16_t(uiZ)
		}

	}
	sig = (sig + roundIncrement) >> 4
	var roundNearEven uint16 = 1 // Assuming roundNearEven is 1
	temp := uint16(0)
	if (uint16(roundBits) ^ 8) == 0 {
		temp = 1
	}
	sig &= ^(uint16(temp & roundNearEven))
	if sig == 0 {
		exp = 0
	}
	uiZ = PackToF16UI(sign, uint16(exp), sig)
	return Float16_t(uiZ)
}
