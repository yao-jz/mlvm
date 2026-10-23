package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F16_mul(a Float16_t, b Float16_t) Float16_t {
	var uiA, uiB, sigA, sigB, sigZ, uiZ uint16
	var signA, signB, signZ bool
	var expA, expB, expZ int8
	var sig32Z uint32
	var magBits uint16

	uiA = uint16(a)
	signA = SignF16UI(uiA)
	expA = ExpF16UI(uiA)
	sigA = FracF16UI(uiA)

	uiB = uint16(b)
	signB = SignF16UI(uiB)
	expB = ExpF16UI(uiB)
	sigB = FracF16UI(uiB)

	signZ = signA != signB

	if expA == 0x1F {
		if sigA != 0 || (expB == 0x1F && sigB != 0) {
			// propagateNaN
			uiZ = Softfloat_propagateNaNF16UI(uiA, uiB)
			// uiZ
			return Float16_t(uiZ)
		}
		magBits = uint16(expB) | sigB
		// infArg
		if magBits == 0 {
			uiZ = 0x7E00
		} else {
			uiZ = PackToF16UI(signZ, 0x1F, 0)
		}
		// uiZ
		return Float16_t(uiZ)
	}

	if expB == 0x1F {
		if sigB != 0 {
			// propagateNaN
			uiZ = Softfloat_propagateNaNF16UI(uiA, uiB)
			// uiZ
			return Float16_t(uiZ)
		}
		magBits = uint16(expA) | sigA
		// infArg
		if magBits == 0 {
			uiZ = 0x7E00
		} else {
			uiZ = PackToF16UI(signZ, 0x1F, 0)
		}
		// uiZ
		return Float16_t(uiZ)
	}

	if expA == 0 {
		if sigA == 0 {
			// zero
			uiZ = PackToF16UI(signZ, 0, 0)
			// uiZ
			return Float16_t(uiZ)
		}
		expA, sigA = Softfloat_normSubnormalF16Sig(sigA)
	}

	if expB == 0 {
		if sigB == 0 {
			// zero
			uiZ = PackToF16UI(signZ, 0, 0)
			// uiZ
			return Float16_t(uiZ)
		}
		expB, sigB = Softfloat_normSubnormalF16Sig(sigB)
	}

	expZ = expA + expB - 0xF
	sigA = (sigA | 0x0400) << 4
	sigB = (sigB | 0x0400) << 5
	sig32Z = uint32(sigA) * uint32(sigB)
	sigZ = uint16(sig32Z >> 16)
	if sig32Z&0xFFFF != 0 {
		sigZ |= 1
	}
	if sigZ < 0x4000 {
		expZ--
		sigZ <<= 1
	}
	return Softfloat_roundPackToF16(signZ, int16(expZ), sigZ)
}
