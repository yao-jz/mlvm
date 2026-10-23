package include

const WordIncr = 1

func IndexWord(total, n int) int {
	return n
}

func IndexWordHi(total int) int {
	return total - 1
}

func IndexWordLo(total int) int {
	return 0
}

func IndexMultiword(total, m, n int) int {
	return n
}

func IndexMultiwordHi(total, n int) int {
	return total - n
}

func IndexMultiwordLo(total, n int) int {
	return 0
}

func IndexMultiwordHiBut(total, n int) int {
	return n
}

func IndexMultiwordLoBut(total, n int) int {
	return 0
}

func InitUintM4(v3, v2, v1, v0 uint32) [4]uint32 {
	return [4]uint32{v0, v1, v2, v3}
}
