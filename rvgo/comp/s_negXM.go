package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_negXM(size_words uint8, zPtr []uint32) {
	var index, lastIndex uint
	var carry uint8
	var word uint32

	index = uint(IndexWordLo(int(size_words)))
	lastIndex = uint(IndexWordHi(int(size_words)))
	carry = 1
	for {
		word = ^zPtr[index] + uint32(carry)
		zPtr[index] = word
		if index == lastIndex {
			break
		}
		index++
		if word != 0 {
			carry = 0
		}
	}
}
