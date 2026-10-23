package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func Softfloat_addMagsF16(uiA uint16, uiB uint16) Float16_t {
	var expA, expB, expDiff int8
	var sigA, sigB, uiZ, sigZ, sigX, sigY uint16
	var signZ bool
	var expZ int8
	var sig32Z uint32
	var shiftDist int8

	expA = ExpF16UI(uiA)
	sigA = FracF16UI(uiA)
	expB = ExpF16UI(uiB)
	sigB = FracF16UI(uiB)

	expDiff = expA - expB
	if expDiff == 0 {
		if expA == 0 {
			uiZ = uiA + sigB
			// uiZ
			return Float16_t(uiZ)
		}
		if expA == 0x1F {
			if (sigA | sigB) != 0 {
				// propagateNaN
				uiZ = Softfloat_propagateNaNF16UI(uiA, uiB)
				// uiZ
				return Float16_t(uiZ)
			}
			uiZ = uiA
			// uiZ
			return Float16_t(uiZ)
		}
		signZ = SignF16UI(uiA)
		expZ = expA
		sigZ = 0x0800 + sigA + sigB
		if (sigZ&1) == 0 && expZ < 0x1E {
			sigZ >>= 1
			// pack
			uiZ = PackToF16UI(signZ, uint16(expZ), sigZ)
			// uiZ
			return Float16_t(uiZ)
		}
		sigZ <<= 3
	} else {
		signZ = SignF16UI(uiA)
		if expDiff < 0 {
			if expB == 0x1F {
				if sigB != 0 {
					// propagateNaN
					uiZ = Softfloat_propagateNaNF16UI(uiA, uiB)
					// uiZ
					return Float16_t(uiZ)
				}
				uiZ = PackToF16UI(signZ, 0x1F, 0)
				// uiZ
				return Float16_t(uiZ)
			}
			if expDiff <= -13 {
				uiZ = PackToF16UI(signZ, uint16(expB), sigB)
				if (uint16(expA) | sigA) != 0 {
					// addEpsilon
					// uiZ
					return Float16_t(uiZ)
				}
				// uiZ
				return Float16_t(uiZ)
			}
			expZ = expB
			sigX = sigB | 0x0400
			if expA != 0 {
				sigY = sigA + 0x0400
			} else {
				sigY = sigA + sigA
			}
			shiftDist = 19 + expDiff
		} else {
			uiZ = uiA
			if expA == 0x1F {
				if sigA != 0 {
					// propagateNaN
					uiZ = Softfloat_propagateNaNF16UI(uiA, uiB)
					// uiZ
					return Float16_t(uiZ)
				}
				// uiZ
				return Float16_t(uiZ)
			}
			if 13 <= expDiff {
				if (uint16(expB) | sigB) != 0 {
					// addEpsilon
					// uiZ
					return Float16_t(uiZ)
				}
				// uiZ
				return Float16_t(uiZ)
			}
			expZ = expA
			sigX = sigA | 0x0400
			if expB != 0 {
				sigY = sigB + 0x0400
			} else {
				sigY = sigB + sigB
			}
			shiftDist = 19 - expDiff
		}
		sig32Z = (uint32(sigX) << 19) + (uint32(sigY) << uint32(shiftDist))
		if sig32Z < 0x40000000 {
			expZ--
			sig32Z <<= 1
		}
		sigZ = uint16(sig32Z >> 16)
		if (sig32Z & 0xFFFF) != 0 {
			sigZ |= 1
		} else {
			if (sigZ&0xF) == 0 && expZ < 0x1E {
				sigZ >>= 4
				// pack
				uiZ = PackToF16UI(signZ, uint16(expZ), sigZ)
				// uiZ
				return Float16_t(uiZ)
			}
		}
	}
	return Softfloat_roundPackToF16(signZ, int16(expZ), sigZ)
}
