package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func Softfloat_subMagsF64(uiA uint64, uiB uint64, signZ bool) Float64_t {
	var expA int16
	var sigA uint64
	var expB int16
	var sigB uint64
	var expDiff int16
	var sigDiff int64
	var shiftDist int8
	var expZ int16
	var sigZ uint64
	var uiZ uint64

	/*------------------------------------------------------------------------
	 *------------------------------------------------------------------------*/
	expA = ExpF64UI(uiA)
	sigA = FracF64UI(uiA)
	expB = ExpF64UI(uiB)
	sigB = FracF64UI(uiB)
	expDiff = expA - expB
	if expDiff == 0 {
		if expA == 0x7FF {
			if (sigA | sigB) != 0 {
				// propagateNaN
				uiZ = Softfloat_propagateNaNF64UI(uiA, uiB)
				// uiZ
				return Float64_t(uiZ)
			}
			// invalid: infinity minus infinity
			Softfloat_raiseFlags(Softfloat_flag_invalid)
			uiZ = 0x7FF8000000000000
			// uiZ
			return Float64_t(uiZ)
		}
		sigDiff = int64(sigA - sigB)
		if sigDiff == 0 {
			uiZ = PackToF64UI(Softfloat_roundingMode == Softfloat_round_min, 0, 0)
			// uiZ
			return Float64_t(uiZ)
		}
		if expA != 0 {
			expA -= 1
		}
		if sigDiff < 0 {
			signZ = !signZ
			sigDiff = -sigDiff
		}
		shiftDist = int8(Softfloat_countLeadingZeros64(uint64(sigDiff)) - 11)
		expZ = expA - int16(shiftDist)
		if expZ < 0 {
			shiftDist = int8(expA)
			expZ = 0
		}
		uiZ = PackToF64UI(signZ, uint64(expZ), uint64(sigDiff)<<uint64(shiftDist))
		// uiZ
		return Float64_t(uiZ)
	} else {
		sigA <<= 10
		sigB <<= 10
		if expDiff < 0 {
			signZ = !signZ
			if expB == 0x7FF {
				if sigB != 0 {
					// propagateNaN
					uiZ = Softfloat_propagateNaNF64UI(uiA, uiB)
					// uiZ
					return Float64_t(uiZ)
				}
				uiZ = PackToF64UI(signZ, 0x7FF, 0)
				// uiZ
				return Float64_t(uiZ)
			}
			if expA != 0 {
				sigA += 0x4000000000000000
			} else {
				sigA += sigA
			}
			sigA = Softfloat_shiftRightJam64(sigA, uint32(-expDiff))
			sigB |= uint64(0x4000000000000000)
			expZ = expB
			sigZ = sigB - sigA
		} else {
			if expA == 0x7FF {
				if sigA != 0 {
					// propagateNaN
					uiZ = Softfloat_propagateNaNF64UI(uiA, uiB)
					// uiZ
					return Float64_t(uiZ)
				}
				uiZ = uiA
				// uiZ
				return Float64_t(uiZ)
			}
			if expB != 0 {
				sigB += 0x4000000000000000
			} else {
				sigB += sigB
			}
			sigB = Softfloat_shiftRightJam64(sigB, uint32(expDiff))
			sigA |= uint64(0x4000000000000000)
			expZ = expA
			sigZ = sigA - sigB
		}
		return Softfloat_normRoundPackToF64(signZ, expZ-1, sigZ)
	}

}
