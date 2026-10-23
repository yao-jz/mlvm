package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F16_sqrt(a Float16_t) Float16_t {
	var uiA, sigA, uiZ, r0, recipSqrt16, sigZ, shiftedSigZ, negRem uint16
	var signA bool
	var expA, expZ, index int8
	var ESqrR0 uint32
	var sigma0 uint16

	uiA = uint16(a)
	signA = SignF16UI(uiA)
	expA = ExpF16UI(uiA)
	sigA = FracF16UI(uiA)

	if expA == 0x1F {
		if sigA != 0 {
			uiZ = Softfloat_propagateNaNF16UI(uiA, 0)
			// uiZ
			return Float16_t(uiZ)
		}
		if !signA {
			return a
		}
		// invalid
		uiZ = 0x7E00
		// uiZ
		return Float16_t(uiZ)
	}

	if signA {
		if uint16(expA)|sigA == 0 {
			return a
		}
		// invalid
		uiZ = 0x7E00
		// uiZ
		return Float16_t(uiZ)
	}

	if expA == 0 {
		if sigA == 0 {
			return a
		}
		expA, sigA = Softfloat_normSubnormalF16Sig(sigA)
	}

	expZ = ((expA - 0xF) >> 1) + 0xE
	expA &= 1
	sigA |= 0x0400
	index = int8((sigA >> 6 & 0xE) + uint16(expA))
	r0 = Softfloat_approxRecipSqrt_1k0s[index] - uint16(((uint32(Softfloat_approxRecipSqrt_1k1s[index]) * uint32(sigA&0x7F)) >> 11))
	ESqrR0 = uint32(r0) * uint32(r0) >> 1
	if expA != 0 {
		ESqrR0 >>= 1
	}
	sigma0 = ^uint16((ESqrR0 * uint32(sigA)) >> 16)
	recipSqrt16 = r0 + uint16((uint32(r0)*uint32(sigma0))>>25)
	if recipSqrt16&0x8000 == 0 {
		recipSqrt16 = 0x8000
	}
	sigZ = uint16(uint32(sigA<<5) * uint32(recipSqrt16) >> 16)
	if expA != 0 {
		sigZ >>= 1
	}

	sigZ++
	if sigZ&7 == 0 {
		shiftedSigZ = sigZ >> 1
		negRem = shiftedSigZ * shiftedSigZ
		sigZ &= ^uint16(1)
		if negRem&0x8000 != 0 {
			sigZ |= 1
		} else {
			if negRem != 0 {
				sigZ--
			}
		}
	}
	return Softfloat_roundPackToF16(false, int16(expZ), sigZ)
}
