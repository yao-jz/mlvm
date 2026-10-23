package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_normSubnormalF32Sig(sig uint32) (int16, uint32) {
	var shiftDist int8
	var result1 int16
	var result2 uint32
	shiftDist = int8(Softfloat_countLeadingZeros32(sig) - 8)
	result1 = int16(1 - shiftDist)
	result2 = sig << uint(shiftDist)
	return result1, result2
}
