package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F64_sqrt(a Float64_t) Float64_t {
	var uiA, sigA, uiZ, rem, sigZ, shiftedSigZ uint64
	var signA bool
	var expA, expZ int16
	var sig32A, recipSqrt32, sig32Z, q uint32

	uiA = uint64(a)
	signA = SignF64UI(uiA)
	expA = ExpF64UI(uiA)
	sigA = FracF64UI(uiA)

	if expA == 0x7FF {
		if sigA != 0 {
			uiZ = Softfloat_propagateNaNF64UI(uiA, 0)
			// uiZ
			return Float64_t(uiZ)
		}
		if !signA {
			return a
		}
		// invalid: square root of a negative number
		Softfloat_raiseFlags(Softfloat_flag_invalid)
		uiZ = 0x7FF8000000000000
		// uiZ
		return Float64_t(uiZ)
	}

	if signA {
		if uint64(expA)|sigA == 0 {
			return a
		}
		// invalid: square root of a negative number
		Softfloat_raiseFlags(Softfloat_flag_invalid)
		uiZ = 0x7FF8000000000000
		// uiZ
		return Float64_t(uiZ)
	}

	if expA == 0 {
		if sigA == 0 {
			return a
		}
		expA, sigA = Softfloat_normSubnormalF64Sig(sigA)
	}

	expZ = ((expA - 0x3FF) >> 1) + 0x3FE
	expA &= 1
	sigA |= 0x0010000000000000
	sig32A = uint32(sigA >> 21)
	recipSqrt32 = Softfloat_approxRecipSqrt32_1(uint32(expA), sig32A)
	sig32Z = uint32((uint64(sig32A) * uint64(recipSqrt32)) >> 32)
	if expA != 0 {
		sigA <<= 8
		sig32Z >>= 1
	} else {
		sigA <<= 9
	}
	rem = sigA - uint64(sig32Z)*uint64(sig32Z)
	q = uint32((rem >> 2) * uint64(recipSqrt32) >> 32)
	sigZ = (uint64(sig32Z)<<32 | 1<<5) + (uint64(q) << 3)

	if (sigZ & 0x1FF) < 0x22 {
		sigZ &= ^uint64(0x3F)
		shiftedSigZ = sigZ >> 6
		rem = (sigA << 52) - shiftedSigZ*shiftedSigZ
		if rem&0x8000000000000000 != 0 {
			sigZ--
		} else {
			if rem != 0 {
				sigZ |= 1
			}
		}
	}
	return Softfloat_roundPackToF64(false, expZ, sigZ)
}
