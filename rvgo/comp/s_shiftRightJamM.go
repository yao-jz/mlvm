package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func Softfloat_shiftRightJamM(size_words uint8, aPtr []uint32, dist uint32, zPtr []uint32) {
	var wordJam, wordDist uint32
	var ptr []uint32
	var i uint8
	var innerDist uint8

	wordJam = 0
	wordDist = dist >> 5
	if wordDist != 0 {
		// fmt.Println("wordDist != 0")
		if uint32(size_words) < wordDist {
			wordDist = uint32(size_words)
		}
		// fmt.Println("wordDist:", wordDist)
		// fmt.Println(IndexMultiwordLo(int(size_words), int(wordDist)))
		ptr = aPtr[IndexMultiwordLo(int(size_words), int(wordDist)):]
		i = uint8(wordDist)
		wordJam = ptr[0]
		ptr = ptr[1:]
		if wordJam != 0 {
			// skip the loop
			// fmt.Println("wordJam != 0, skip the loop")
		} else {
			i--
			for i != 0 {
				wordJam = ptr[0]
				ptr = ptr[1:]
				if wordJam != 0 {
					break
				}
				i--
			}
		}
		ptr = zPtr
		// fmt.Println(ptr) // [0 0 3291037696 1423871281]
	}
	if wordDist < uint32(size_words) {
		// fmt.Println("wordDist < uint32(size_words)")
		aPtr = aPtr[uint(IndexMultiwordHiBut(int(size_words), int(wordDist))):]
		innerDist = uint8(dist & 31)
		if innerDist != 0 {
			// fmt.Println("innerDist != 0")
			Softfloat_shortShiftRightJamM(
				uint8(uint32(size_words)-wordDist),
				aPtr,
				uint8(innerDist),
				zPtr[IndexMultiwordLoBut(int(size_words), int(wordDist)):],
			)
			if wordDist == 0 {
				// wordJam
				if wordJam != 0 {
					zPtr[IndexWordLo(int(size_words))] |= 1
				}
				return
			}
		} else {
			// fmt.Println("innerDist == 0")
			aPtr = aPtr[IndexWordLo(int(uint32(size_words)-wordDist)):]
			ptr = zPtr[IndexWordLo(int(size_words)):]
			for i = uint8(uint32(size_words) - wordDist); i > 0; i-- {
				ptr[0] = aPtr[0]
				aPtr = aPtr[1:]
				ptr = ptr[1:]
			}
		}
		ptr = zPtr[IndexMultiwordHi(int(size_words), int(wordDist)):]
	}

	// fmt.Println(ptr) // []
	ptr[0] = 0
	ptr = ptr[1:]
	wordDist--
	for wordDist != 0 {
		ptr[0] = 0
		ptr = ptr[1:]
		wordDist--
	}
	// wordJam:
	//
	if wordJam != 0 {
		zPtr[IndexWordLo(int(size_words))] |= 1
	}
}
