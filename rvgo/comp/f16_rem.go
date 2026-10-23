package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F16_rem(a Float16_t, b Float16_t) Float16_t {
	var uiA, uiB, sigA, sigB, rem, q, altRem, meanRem, uiZ uint16
	var signA, signRem bool
	var expA, expB, expDiff int8
	var recip32, q32 uint32

	uiA = uint16(a)
	signA = SignF16UI(uiA)
	expA = ExpF16UI(uiA)
	sigA = FracF16UI(uiA)

	uiB = uint16(b)
	expB = ExpF16UI(uiB)
	sigB = FracF16UI(uiB)

	if expA == 0x1F {
		if sigA != 0 || (expB == 0x1F && sigB != 0) {
			// propagateNaN
			uiZ = Softfloat_propagateNaNF16UI(uiA, uiB)
			// uiZ
			return Float16_t(uiZ)
		}
		// invalid
		uiZ = 0x7E00
		return Float16_t(uiZ)
	}

	if expB == 0x1F {
		if sigB != 0 {
			// propagateNaN
			uiZ = Softfloat_propagateNaNF16UI(uiA, uiB)
			// uiZ
			return Float16_t(uiZ)
		}
		return a
	}

	if expB == 0 {
		if sigB == 0 {
			// invalid
			uiZ = 0x7E00
			return Float16_t(uiZ)
		}
		expB, sigB = Softfloat_normSubnormalF16Sig(sigB)
	}

	if expA == 0 {
		if sigA == 0 {
			return a
		}
		expA, sigA = Softfloat_normSubnormalF16Sig(sigA)
	}

	rem = sigA | 0x0400
	sigB |= 0x0400
	expDiff = expA - expB
	if expDiff < 1 {
		if expDiff < -1 {
			return a
		}
		sigB <<= 3
		if expDiff != 0 {
			rem <<= 2
			q = 0
		} else {
			rem <<= 3
			q = 0
			if sigB <= rem {
				q = 1
				rem -= sigB
			}
		}
	} else {
		recip32 = Softfloat_approxRecip32_1(uint32(sigB) << 21)
		rem <<= 4
		expDiff -= 31
		sigB <<= 3
		for {
			q32 = uint32((uint64(rem) * uint64(recip32)) >> 16)
			if expDiff < 0 {
				break
			}
			rem = -(uint16(q32) * sigB)
			expDiff -= 29
		}
		q32 >>= ^expDiff & 31
		q = uint16(q32)
		rem = (rem << (expDiff + 30)) - q*sigB
	}

	altRem = rem
	q++
	rem -= sigB
	for rem&0x8000 == 0 {
		altRem = rem
		q++
		rem -= sigB
	}
	meanRem = rem + altRem
	if meanRem&0x8000 != 0 || (meanRem == 0 && q&1 != 0) {
		rem = altRem
	}
	signRem = signA
	if rem >= 0x8000 {
		signRem = !signRem
		rem = -rem
	}
	return Softfloat_normRoundPackToF16(signRem, int16(expB), rem)
}
