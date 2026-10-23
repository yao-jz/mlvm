package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_shiftLeftM(size_words uint8, aPtr []uint32, dist uint32, zPtr []uint32) {

	wordDist := dist >> 5
	if wordDist < uint32(size_words) {
		aPtr = aPtr[IndexMultiwordLoBut(int(size_words), int(wordDist)):]
		innerDist := dist & 31
		if innerDist != 0 {
			Softfloat_shortShiftLeftM(uint8(uint32(size_words)-wordDist), aPtr, uint8(innerDist), zPtr[IndexMultiwordHiBut(int(size_words), int(wordDist)):])
			if wordDist == 0 {
				return
			}
		} else {
			// correct?
			aPtr = aPtr[:IndexWordHi(int(uint32(size_words)-wordDist))+1]
			destPtr := zPtr[:IndexWordHi(int(size_words))+1]
			for i := int(uint32(size_words) - wordDist); i > 0; i-- {
				destPtr[i] = aPtr[i]
			}
		}
		zPtr = zPtr[IndexMultiwordLo(int(size_words), int(wordDist)):]
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
