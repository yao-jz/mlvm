package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_addM(size_words uint8, aPtr []uint32, bPtr []uint32, zPtr []uint32) {
	var index, lastIndex uint
	var carry uint8
	var wordA, wordZ uint32

	index = uint(IndexWordLo(int(size_words)))
	lastIndex = uint(IndexWordHi(int(size_words)))
	carry = 0
	for {
		wordA = aPtr[index]
		wordZ = wordA + bPtr[index] + uint32(carry)
		zPtr[index] = wordZ
		if index == lastIndex {
			break
		}
		if wordZ != wordA {
			carry = uint8(BoolToInt(wordZ < wordA))
		}
		index++
	}
}
