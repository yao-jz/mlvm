package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func Softfloat_addMagsF32(uiA uint32, uiB uint32) Float32_t {
	var expA, expB, expDiff int16
	var sigA, sigB, uiZ, sigZ uint32
	var signZ bool
	var expZ int16

	expA = ExpF32UI(uiA)
	sigA = FracF32UI(uiA)
	expB = ExpF32UI(uiB)
	sigB = FracF32UI(uiB)

	expDiff = expA - expB
	if expDiff == 0 {
		if expA == 0 {
			uiZ = uiA + sigB
			// uiZ
			return Float32_t(uiZ)
		}
		if expA == 0xFF {
			if (sigA | sigB) != 0 {
				// propagateNaN
				uiZ = Softfloat_propagateNaNF32UI(uiA, uiB)
				// uiZ
				return Float32_t(uiZ)
			}
			uiZ = uiA
			// uiZ
			return Float32_t(uiZ)
		}
		signZ = SignF32UI(uiA)
		expZ = expA
		sigZ = 0x01000000 + sigA + sigB
		if sigZ&1 == 0 && expZ < 0xFE {
			uiZ = PackToF32UI(signZ, uint32(expZ), sigZ>>1)
			// uiZ
			return Float32_t(uiZ)
		}
		sigZ <<= 6
	} else {
		signZ = SignF32UI(uiA)
		sigA <<= 6
		sigB <<= 6
		if expDiff < 0 {
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
			expZ = expB
			if expA != 0 {
				sigA += 0x20000000
			} else {
				sigA += sigA
			}
			sigA = Softfloat_shiftRightJam32(sigA, uint16(-expDiff))
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
			expZ = expA
			if expB != 0 {
				sigB += 0x20000000
			} else {
				sigB += sigB
			}
			sigB = Softfloat_shiftRightJam32(sigB, uint16(expDiff))
		}
		sigZ = 0x20000000 + sigA + sigB
		if sigZ < 0x40000000 {
			expZ--
			sigZ <<= 1
		}
	}
	return Softfloat_roundPackToF32(signZ, expZ, sigZ)
}
