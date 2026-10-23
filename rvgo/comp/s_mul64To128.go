package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_mul64To128(a uint64, b uint64) (uint64, uint64) {
	var a32, a0, b32, b0 uint32
	var v0, v64 uint64
	var mid1, mid uint64

	a32 = uint32(a >> 32)
	a0 = uint32(a)
	b32 = uint32(b >> 32)
	b0 = uint32(b)
	v0 = uint64(a0) * uint64(b0)
	mid1 = uint64(a32) * uint64(b0)
	mid = mid1 + uint64(a0)*uint64(b32)
	v64 = uint64(a32) * uint64(b32)
	v64 += uint64(BoolToInt(mid < mid1))<<32 | mid>>32
	mid <<= 32
	v0 += mid
	v64 += uint64(BoolToInt(v0 < mid))
	return v0, v64
}
