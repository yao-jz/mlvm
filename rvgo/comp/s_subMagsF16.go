package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func Softfloat_subMagsF16(uiA, uiB uint16) Float16_t {
	var expA int8
	var sigA uint16
	var expB int8
	var sigB uint16
	var expDiff int8
	var sigDiff int16
	var signZ bool
	var shiftDist, expZ int8
	var sigZ, sigX, sigY uint16
	var sig32Z uint32
	var uiZ uint16

	/*------------------------------------------------------------------------
	 *------------------------------------------------------------------------*/
	expA = ExpF16UI(uiA)
	sigA = FracF16UI(uiA)
	expB = ExpF16UI(uiB)
	sigB = FracF16UI(uiB)
	expDiff = expA - expB
	if expDiff == 0 {
		if expA == 0x1F {
			if (sigA | sigB) != 0 {
				// propagateNaN
				uiZ = Softfloat_propagateNaNF16UI(uiA, uiB)
				// uiZ
				return Float16_t(uiZ)
			}
			uiZ = 0x7E00
			// uiZ
			return Float16_t(uiZ)
		}
		sigDiff = int16(sigA - sigB)
		if sigDiff == 0 {
			uiZ = PackToF16UI(false, 0, 0)
			// uiZ
			return Float16_t(uiZ)
		}
		if expA != 0 {
			expA -= 1
		}
		signZ = SignF16UI(uiA)
		if sigDiff < 0 {
			signZ = !signZ
			sigDiff = -sigDiff
		}
		shiftDist = int8(Softfloat_countLeadingZeros16(uint16(sigDiff)) - 5)
		expZ = expA - shiftDist
		if expZ < 0 {
			shiftDist = expA
			expZ = 0
		}
		sigZ = uint16(sigDiff << uint16(shiftDist))
		// pack
		uiZ = PackToF16UI(signZ, uint16(expZ), sigZ)
		// uiZ
		return Float16_t(uiZ)
	} else {
		signZ = SignF16UI(uiA)
		if expDiff < 0 {
			signZ = !signZ
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
					// subEpsilon
					// uiZ
					return Float16_t(uiZ)
				}
				// uiZ
				return Float16_t(uiZ)
			}
			expZ = expA + 19
			sigX = sigB | 0x0400
			if expA != 0 {
				sigY = sigA + 0x0400
			} else {
				sigY = sigA + sigA
			}
			expDiff = -expDiff
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
					// subEpsilon
					// uiZ
					return Float16_t(uiZ)
				}
				// uiZ
				return Float16_t(uiZ)
			}
			expZ = expB + 19
			sigX = sigA | 0x0400
			if expB != 0 {
				sigY = sigB + 0x0400
			} else {
				sigY = sigB + sigB
			}
		}
		sig32Z = (uint32(sigX) << uint32(expDiff)) - uint32(sigY)
		shiftDist = int8(Softfloat_countLeadingZeros32(sig32Z) - 1)
		sig32Z <<= uint32(shiftDist)
		expZ -= shiftDist
		sigZ = uint16(sig32Z >> 16)
		if (sig32Z & 0xFFFF) != 0 {
			sigZ |= 1
		} else {
			if ((sigZ & 0xF) == 0) && (uint32(expZ) < 0x1E) {
				sigZ >>= 4
				// pack
				uiZ = PackToF16UI(signZ, uint16(expZ), sigZ)
				// uiZ
				return Float16_t(uiZ)
			}
		}
		return Softfloat_roundPackToF16(signZ, int16(expZ), sigZ)
	}
	// // propagateNaN
	// uiZ = Softfloat_propagateNaNF16UI(uiA, uiB)
	// // uiZ
	// return Float16_t(uiZ)
	// // subEpsilon
	// // uiZ
	// return Float16_t(uiZ)
}
