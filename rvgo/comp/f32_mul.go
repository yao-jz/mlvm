package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F32_mul(a Float32_t, b Float32_t) Float32_t {
	var uiA, uiB uint32
	var signA, signB, signZ bool
	var expA, expB, expZ int16
	var sigA, sigB, sigZ uint32
	var magBits uint32
	var uiZ uint32

	uiA = uint32(a)
	signA = SignF32UI(uiA)
	expA = ExpF32UI(uiA)
	sigA = FracF32UI(uiA)
	uiB = uint32(b)
	signB = SignF32UI(uiB)
	expB = ExpF32UI(uiB)
	sigB = FracF32UI(uiB)
	signZ = signA != signB

	if expA == 0xFF {
		if sigA != 0 || (expB == 0xFF && sigB != 0) {
			// propagateNaN
			uiZ = Softfloat_propagateNaNF32UI(uiA, uiB)
			// uiZ
			return Float32_t(uiZ)
		}
		magBits = uint32(expB) | sigB
		// infArg
		if magBits == 0 {
			// invalid: zero times infinity
			Softfloat_raiseFlags(Softfloat_flag_invalid)
			uiZ = 0x7FC00000
		} else {
			uiZ = PackToF32UI(signZ, 0xFF, 0)
		}
		// uiZ
		return Float32_t(uiZ)
	}
	if expB == 0xFF {
		if sigB != 0 {
			// propagateNaN
			uiZ = Softfloat_propagateNaNF32UI(uiA, uiB)
			// uiZ
			return Float32_t(uiZ)
		}
		magBits = uint32(expA) | sigA
		// infArg
		if magBits == 0 {
			// invalid: zero times infinity
			Softfloat_raiseFlags(Softfloat_flag_invalid)
			uiZ = 0x7FC00000
		} else {
			uiZ = PackToF32UI(signZ, 0xFF, 0)
		}
		// uiZ
		return Float32_t(uiZ)
	}

	if expA == 0 {
		if sigA == 0 {
			// zero
			uiZ = PackToF32UI(signZ, 0, 0)
			// uiZ
			return Float32_t(uiZ)
		}
		expA, sigA = Softfloat_normSubnormalF32Sig(sigA)
	}
	if expB == 0 {
		if sigB == 0 {
			// zero
			uiZ = PackToF32UI(signZ, 0, 0)
			// uiZ
			return Float32_t(uiZ)
		}
		expB, sigB = Softfloat_normSubnormalF32Sig(sigB)
	}

	expZ = expA + expB - 0x7F
	sigA = (sigA | 0x00800000) << 7
	sigB = (sigB | 0x00800000) << 8
	sigZ = uint32(Softfloat_shortShiftRightJam64(uint64(sigA)*uint64(sigB), 32))
	if sigZ < 0x40000000 {
		expZ--
		sigZ <<= 1
	}
	return Softfloat_roundPackToF32(signZ, expZ, sigZ)
}
