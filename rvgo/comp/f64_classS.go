package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
)

func F64_classS(value Float64_t) uint64 {
	var result uint64

	sign := (value >> 63) & 0x1
	exp := (value >> 52) & 0x7FF
	frac := value & 0xFFFFFFFFFFFFF

	if exp == 0x7FF && frac != 0 {
		// NaN
		if frac&(1<<51) != 0 {
			result = 1 << 9 // Quiet NaN
		} else {
			result = 1 << 8 // Signaling NaN
		}
	} else if exp == 0x7FF && frac == 0 {
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
	} else if exp > 0 && exp < 0x7FF {
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
