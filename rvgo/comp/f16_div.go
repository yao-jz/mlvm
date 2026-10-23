package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F16_div(a Float16_t, b Float16_t) Float16_t {
	var uiA uint16
	var signA bool
	var expA int8
	var sigA uint16
	var uiB uint16
	var signB bool
	var expB int8
	var sigB uint16
	var signZ bool
	var expZ int8

	var index int
	var r0 uint16
	var sigZ, rem uint16
	var uiZ uint16

	uiA = uint16(a)
	signA = SignF16UI(uiA)
	expA = ExpF16UI(uiA)
	sigA = FracF16UI(uiA)
	uiB = uint16(b)
	signB = SignF16UI(uiB)
	expB = ExpF16UI(uiB)
	sigB = FracF16UI(uiB)
	signZ = IntToBool(BoolToInt(signA) ^ BoolToInt(signB))

	if expA == 0x1F {
		if sigA != 0 {
			// propagateNaN
			uiZ = Softfloat_propagateNaNF16UI(uiA, uiB)
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
			// invalid
			uiZ = 0x7E00
			// uiZ
			return Float16_t(uiZ)
		}
		// infinity
		uiZ = PackToF16UI(signZ, 0x1F, 0)
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
		// zero
		uiZ = PackToF16UI(signZ, 0, 0)
		// uiZ
		return Float16_t(uiZ)
	}
	if expB == 0 {
		if sigB == 0 {
			if (uint16(expA) | sigA) == 0 {
				// invalid
				uiZ = 0x7E00
				// uiZ
				return Float16_t(uiZ)
			}
			// infinity
			uiZ = PackToF16UI(signZ, 0x1F, 0)
			// uiZ
			return Float16_t(uiZ)
		}
		expB, sigB = Softfloat_normSubnormalF16Sig(sigB)
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

	expZ = expA - expB + 0xE
	sigA |= 0x0400
	sigB |= 0x0400
	if sigA < sigB {
		expZ--
		sigA <<= 5
	} else {
		sigA <<= 4
	}
	index = int(sigB >> 6 & 0xF)
	r0 = uint16(uint32(Softfloat_approxRecip_1k0s[index]) - ((uint32(Softfloat_approxRecip_1k1s[index]) * uint32(sigB&0x3F)) >> 10))
	sigZ = uint16((uint32(sigA) * uint32(r0)) >> 16)
	rem = (sigA << 10) - sigZ*sigB
	sigZ += uint16((uint32(rem) * uint32(r0)) >> 26)
	sigZ++
	if (sigZ & 7) == 0 {
		sigZ &= ^uint16(1)
		rem = (sigA << 10) - sigZ*sigB
		if rem&0x8000 != 0 {
			sigZ -= 2
		} else {
			if rem != 0 {
				sigZ |= 1
			}
		}
	}
	return Softfloat_roundPackToF16(signZ, int16(expZ), sigZ)
}
