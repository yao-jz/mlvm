package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func Softfloat_subMagsF32(uiA, uiB uint32) Float32_t {
	var expA int16
	var sigA uint32
	var expB int16
	var sigB uint32
	var expDiff int16
	var sigDiff int32
	var signZ bool
	var shiftDist int8
	var expZ int16
	var sigX, sigY uint32
	var uiZ uint32

	/*------------------------------------------------------------------------
	 *------------------------------------------------------------------------*/
	expA = ExpF32UI(uiA)
	sigA = FracF32UI(uiA)
	expB = ExpF32UI(uiB)
	sigB = FracF32UI(uiB)
	expDiff = expA - expB
	if expDiff == 0 {
		if expA == 0xFF {
			if (sigA | sigB) != 0 {
				// propagateNaN
				uiZ = Softfloat_propagateNaNF32UI(uiA, uiB)
				// uiZ
				return Float32_t(uiZ)
			}
			// invalid: infinity minus infinity
			Softfloat_raiseFlags(Softfloat_flag_invalid)
			uiZ = 0x7FC00000
			// uiZ
			return Float32_t(uiZ)
		}
		sigDiff = int32(sigA - sigB)
		if sigDiff == 0 {
			uiZ = PackToF32UI(false, 0, 0)
			// uiZ
			return Float32_t(uiZ)
		}
		if expA != 0 {
			expA -= 1
		}
		signZ = SignF32UI(uiA)
		if sigDiff < 0 {
			signZ = !signZ
			sigDiff = -sigDiff
		}
		shiftDist = int8(Softfloat_countLeadingZeros32(uint32(sigDiff)) - 8)
		expZ = expA - int16(shiftDist)
		if expZ < 0 {
			shiftDist = int8(expA)
			expZ = 0
		}
		uiZ = PackToF32UI(signZ, uint32(expZ), uint32(sigDiff<<uint32(shiftDist)))
		// uiZ
		return Float32_t(uiZ)
	} else {
		signZ = SignF32UI(uiA)
		sigA <<= 7
		sigB <<= 7
		if expDiff < 0 {
			signZ = !signZ
			if expB == 0xFF {
				if sigB != 0 {
					// propagateNaN
					uiZ = Softfloat_propagateNaNF32UI(uiA, uiB)
					// uiZ
					return Float32_t(uiZ)
				}
				uiZ = PackToF32UI(signZ, 0xFF, 0)
				// uiZ
				return Float32_t(uiZ)
			}
			expZ = expB - 1
			sigX = sigB | 0x40000000
			if expA != 0 {
				sigY = sigA + 0x40000000
			} else {
				sigY = sigA + sigA
			}
			expDiff = -expDiff
		} else {
			if expA == 0xFF {
				if sigA != 0 {
					// propagateNaN
					uiZ = Softfloat_propagateNaNF32UI(uiA, uiB)
					// uiZ
					return Float32_t(uiZ)
				}
				uiZ = uiA
				// uiZ
				return Float32_t(uiZ)
			}
			expZ = expA - 1
			sigX = sigA | 0x40000000
			if expB != 0 {
				sigY = sigB + 0x40000000
			} else {
				sigY = sigB + sigB
			}
		}
		return Softfloat_normRoundPackToF32(signZ, expZ, sigX-Softfloat_shiftRightJam32(sigY, uint16(expDiff)))
	}
	// // propagateNaN
	// uiZ = Softfloat_propagateNaNF32UI(uiA, uiB)
	// // uiZ
	// return Float32_t(uiZ)
}
