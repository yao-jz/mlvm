package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F32_classS(value Float32_t) uint32 {
	var result uint32

	sign := (value >> 31) & 0x1
	exp := (value >> 23) & 0xFF
	frac := value & 0x7FFFFF

	if exp == 0xFF && frac != 0 {
		// NaN
		if frac&(1<<22) != 0 {
			result = 1 << 9 // Quiet NaN
		} else {
			result = 1 << 8 // Signaling NaN
		}
	} else if exp == 0xFF && frac == 0 {
		// Inf
		if sign == 1 {
			result = 1 << 0 // Negative inf
		} else {
			result = 1 << 7 // Positive inf
		}
	} else if exp == 0 && frac != 0 {
		// Subnormal
		if sign == 1 {
			result = 1 << 2 // Negative subnormal
		} else {
			result = 1 << 5 // Positive subnormal
		}
	} else if exp == 0 && frac == 0 {
		// Zero
		if sign == 1 {
			result = 1 << 3 // Negative zero
		} else {
			result = 1 << 4 // Positive zero
		}
	} else if exp > 0 && exp < 0xFF {
		// Normal
		if sign == 1 {
			result = 1 << 1 // Negative normal
		} else {
			result = 1 << 6 // Positive normal
		}
	} else {
		// panic
		result = 0
	}
	return result
}
