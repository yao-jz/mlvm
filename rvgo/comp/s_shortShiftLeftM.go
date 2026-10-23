package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_shortShiftLeftM(size_words uint8, aPtr []uint32, dist uint8, zPtr []uint32) {
	var uNegDist uint8
	var index, lastIndex uint
	var partWordZ, wordA uint32

	uNegDist = -dist
	index = uint(IndexWordHi(int(size_words)))
	lastIndex = uint(IndexWordLo(int(size_words)))
	partWordZ = aPtr[index] << dist
	for index != lastIndex {
		wordA = aPtr[index-1]
		zPtr[index] = partWordZ | (wordA >> (uNegDist & 31))
		index--
		partWordZ = wordA << dist
	}
	zPtr[index] = partWordZ
}
