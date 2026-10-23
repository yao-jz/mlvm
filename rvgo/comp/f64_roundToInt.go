package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F64_roundToInt(a Float64_t, exact bool) Float64_t {
	var uiA, uiZ, lastBitMask, roundBitsMask uint64
	var exp int16

	uiA = uint64(a)
	exp = ExpF64UI(uiA)

	if exp <= 0x3FE {
		if uiA&0x7FFFFFFFFFFFFFFF == 0 {
			return a
		}
		uiZ = uiA & PackToF64UI(true, 0, 0)
		if FracF64UI(uiA) == 0 {
		} else {
			if exp == 0x3FE {
				uiZ |= PackToF64UI(false, 0x3FF, 0)
			}
		}
		// uiZ
		return Float64_t(uiZ)
	}

	if 0x433 <= exp {
		if exp == 0x7FF && FracF64UI(uiA) != 0 {
			uiZ = Softfloat_propagateNaNF64UI(uiA, 0)
			// uiZ
			return Float64_t(uiZ)
		}
		return a
	}

	uiZ = uiA
	lastBitMask = uint64(1) << (0x433 - exp)
	roundBitsMask = lastBitMask - 1
	uiZ += lastBitMask >> 1
	if uiZ&roundBitsMask == 0 {
		uiZ &= ^lastBitMask
	}
	uiZ &= ^roundBitsMask
	// uiZ
	return Float64_t(uiZ)
}
