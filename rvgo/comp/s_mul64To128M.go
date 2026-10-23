package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_mul64To128M(a uint64, b uint64, zPtr []uint32) {
	var a32, a0, b32, b0 uint32
	var z0, mid1, z64, mid uint64

	a32 = uint32(a >> 32)
	a0 = uint32(a)
	b32 = uint32(b >> 32)
	b0 = uint32(b)
	z0 = uint64(a0) * uint64(b0)
	mid1 = uint64(a32) * uint64(b0)
	mid = mid1 + uint64(a0)*uint64(b32)
	z64 = uint64(a32) * uint64(b32)
	z64 += uint64(BoolToInt(mid < mid1))<<32 | mid>>32
	mid <<= 32
	z0 += mid
	zPtr[IndexWord(4, 1)] = uint32(z0 >> 32)
	zPtr[IndexWord(4, 0)] = uint32(z0)
	z64 += uint64(BoolToInt(z0 < mid))
	zPtr[IndexWord(4, 3)] = uint32(z64 >> 32)
	zPtr[IndexWord(4, 2)] = uint32(z64)
}
