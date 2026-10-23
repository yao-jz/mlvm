package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F32_sqrt(a Float32_t) Float32_t {
	var uiA, uiZ, sigA uint32
	var signA bool
	var expA, expZ int16
	var sigZ, shiftedSigZ uint32
	var negRem uint32

	uiA = uint32(a)
	signA = SignF32UI(uiA)
	expA = ExpF32UI(uiA)
	sigA = FracF32UI(uiA)

	if expA == 0xFF {
		if sigA != 0 {
			uiZ = Softfloat_propagateNaNF32UI(uiA, 0)
			// uiZ
			return Float32_t(uiZ)
		}
		if !signA {
			return a
		}
		// invalid: square root of a negative number
		Softfloat_raiseFlags(Softfloat_flag_invalid)
		uiZ = 0x7FC00000
		// uiZ
		return Float32_t(uiZ)
	}

	if signA {
		if uint32(expA)|sigA == 0 {
			return a
		}
		// invalid: square root of a negative number
		Softfloat_raiseFlags(Softfloat_flag_invalid)
		uiZ = 0x7FC00000
		// uiZ
		return Float32_t(uiZ)
	}

	if expA == 0 {
		if sigA == 0 {
			return a
		}
		expA, sigA = Softfloat_normSubnormalF32Sig(sigA)
	}

	expZ = ((expA - 0x7F) >> 1) + 0x7E
	expA &= 1
	sigA = (sigA | 0x00800000) << 8
	sigZ =
		uint32((uint64(sigA) * uint64(Softfloat_approxRecipSqrt32_1(uint32(expA), sigA))) >> 32)
	if expA != 0 {
		sigZ >>= 1
	}
	sigZ += 2
	if (sigZ & 0x3F) < 2 {
		shiftedSigZ = sigZ >> 2
		negRem = shiftedSigZ * shiftedSigZ
		sigZ &= ^uint32(3)
		if negRem&0x80000000 != 0 {
			sigZ |= 1
		} else {
			if negRem != 0 {
				sigZ--
			}
		}
	}
	return Softfloat_roundPackToF32(false, expZ, sigZ)
}
