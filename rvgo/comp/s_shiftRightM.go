package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_shiftRightM(size_words uint8, aPtr []uint32, dist uint32, zPtr []uint32) {
	var wordDist uint32
	var innerDist uint8
	var destPtr []uint32
	var i uint8

	wordDist = dist >> 5
	if wordDist < uint32(size_words) {
		idx := IndexMultiwordHiBut(int(size_words), int(wordDist))
		aPtr = aPtr[idx:]
		innerDist = uint8(dist & 31)
		if innerDist != 0 {
			Softfloat_shortShiftRightM(
				uint8(uint32(size_words)-wordDist),
				aPtr,
				innerDist,
				zPtr[IndexMultiwordLoBut(int(size_words), int(wordDist)):],
			)
			if wordDist == 0 {
				return
			}
		} else {
			aPtr = aPtr[IndexWordLo(int(uint32(size_words)-wordDist)):]
			destPtr = zPtr[IndexWordLo(int(size_words)):]
			for i = uint8(uint32(size_words) - wordDist); i > 0; i-- {
				destPtr[0] = aPtr[0]
				aPtr = aPtr[1:]
				destPtr = destPtr[1:]
			}
		}
		zPtr = zPtr[IndexMultiwordHi(int(size_words), int(wordDist)):]
	} else {
		wordDist = uint32(size_words)
	}
	zPtr[0] = 0
	zPtr = zPtr[1:]
	wordDist--
	for wordDist != 0 {
		zPtr[0] = 0
		zPtr = zPtr[1:]
		wordDist--
	}
}
