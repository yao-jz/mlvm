package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_normSubnormalF16Sig(sig uint16) (int8, uint16) {
	var shiftDist int8
	var result1 int8
	var result2 uint16
	shiftDist = int8(Softfloat_countLeadingZeros16(sig) - 5)
	result1 = 1 - shiftDist
	result2 = sig << uint(shiftDist)
	return result1, result2
}
