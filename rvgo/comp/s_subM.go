package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_subM(size_words uint8, aPtr []uint32, bPtr []uint32, zPtr []uint32) {
	var index, lastIndex uint
	var borrow uint8
	var wordA, wordB uint32

	index = uint(IndexWordLo(int(size_words)))
	lastIndex = uint(IndexWordHi(int(size_words)))
	borrow = 0
	for {
		wordA = aPtr[index]
		wordB = bPtr[index]
		zPtr[index] = wordA - wordB - uint32(borrow)
		if index == lastIndex {
			break
		}
		if borrow != 0 {
			borrow = uint8(BoolToInt(wordA <= wordB))
		} else {
			borrow = uint8(BoolToInt(wordA < wordB))
		}
		index++
	}
}
