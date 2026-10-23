package include

import "math/bits"

func Softfloat_countLeadingZeros16(a uint16) uint8 {
	// in C, __builtin_clz will extend a to 32 bits and then count leading zeros
	if a == 0 {
		return 16
	}
	return uint8(bits.LeadingZeros16(a))
}

func Softfloat_countLeadingZeros32(a uint32) uint8 {
	if a == 0 {
		return 32
	}
	return uint8(bits.LeadingZeros32(a))
}

func Softfloat_countLeadingZeros64(a uint64) uint8 {
	if a == 0 {
		return 64
	}
	return uint8(bits.LeadingZeros64(a))
}
