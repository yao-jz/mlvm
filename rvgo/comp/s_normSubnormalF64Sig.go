package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_normSubnormalF64Sig(sig uint64) (int16, uint64) {
	var shiftDist int8
	var result1 int16
	var result2 uint64
	shiftDist = int8(Softfloat_countLeadingZeros64(sig) - 11)
	result1 = int16(1 - shiftDist)
	result2 = sig << uint(shiftDist)
	return result1, result2
}
