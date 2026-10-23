package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func Softfloat_addMagsF64(uiA uint64, uiB uint64, signZ bool) Float64_t {
	var expA, expB, expDiff int16
	var sigA, sigB, uiZ, sigZ uint64
	var expZ int16

	expA = ExpF64UI(uiA)
	sigA = FracF64UI(uiA)
	expB = ExpF64UI(uiB)
	sigB = FracF64UI(uiB)

	expDiff = expA - expB
	if expDiff == 0 {
		if expA == 0 {
			uiZ = uiA + sigB
			// uiZ
			return Float64_t(uiZ)
		}
		if expA == 0x7FF {
			if (sigA | sigB) != 0 {
				// propagateNaN
				uiZ = Softfloat_propagateNaNF64UI(uiA, uiB)
				// uiZ
				return Float64_t(uiZ)
			}
			uiZ = uiA
			// uiZ
			return Float64_t(uiZ)
		}
		expZ = expA
		sigZ = 0x0020000000000000 + sigA + sigB
		sigZ <<= 9
	} else {
		sigA <<= 9
		sigB <<= 9
		if expDiff < 0 {
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
			expZ = expB
			if expA != 0 {
				sigA += 0x2000000000000000
			} else {
				sigA <<= 1
			}
			sigA = Softfloat_shiftRightJam64(sigA, uint32(-expDiff))
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
			expZ = expA
			if expB != 0 {
				sigB += 0x2000000000000000
			} else {
				sigB <<= 1
			}
			sigB = Softfloat_shiftRightJam64(sigB, uint32(expDiff))
		}
		sigZ = 0x2000000000000000 + sigA + sigB
		if sigZ < 0x4000000000000000 {
			expZ--
			sigZ <<= 1
		}
	}
	return Softfloat_roundPackToF64(signZ, expZ, sigZ)
}
