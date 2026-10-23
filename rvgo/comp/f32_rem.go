package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F32_rem(a Float32_t, b Float32_t) Float32_t {
	var uiA, uiB, uiZ uint32
	var signA, signRem bool
	var expA, expB, expDiff int16
	var sigA, sigB, rem, q, recip32, altRem, meanRem uint32

	uiA = uint32(a)
	signA = SignF32UI(uiA)
	expA = ExpF32UI(uiA)
	sigA = FracF32UI(uiA)
	uiB = uint32(b)
	expB = ExpF32UI(uiB)
	sigB = FracF32UI(uiB)

	if expA == 0xFF {
		if sigA != 0 || (expB == 0xFF && sigB != 0) {
			// propagateNaN
			uiZ = Softfloat_propagateNaNF32UI(uiA, uiB)
			// uiZ
			return Float32_t(uiZ)
		}
		// invalid
		uiZ = 0x7FC00000
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
		return a
	}

	if expB == 0 {
		if sigB == 0 {
			// invalid
			uiZ = 0x7FC00000
			// uiZ
			return Float32_t(uiZ)
		}
		expB, sigB = Softfloat_normSubnormalF32Sig(sigB)
	}
	if expA == 0 {
		if sigA == 0 {
			return a
		}
		expA, sigA = Softfloat_normSubnormalF32Sig(sigA)
	}

	rem = sigA | 0x00800000
	sigB |= 0x00800000
	expDiff = expA - expB
	if expDiff < 1 {
		if expDiff < -1 {
			return a
		}
		sigB <<= 6
		if expDiff != 0 {
			rem <<= 5
			q = 0
		} else {
			rem <<= 6
			q = 0
			if sigB <= rem {
				q = 1
				rem -= sigB
			}
		}
	} else {
		recip32 = Softfloat_approxRecip32_1(sigB << 8)
		rem <<= 7
		expDiff -= 31
		sigB <<= 6
		for {
			q = uint32((uint64(rem) * uint64(recip32)) >> 32)
			if expDiff < 0 {
				break
			}
			rem = -(q * sigB)
			expDiff -= 29
		}
		q >>= uint32(^expDiff & 31)
		rem = (rem << (expDiff + 30)) - q*sigB
	}
	altRem = rem
	q++
	rem -= sigB
	for rem&0x80000000 == 0 {
		altRem = rem
		q++
		rem -= sigB
	}

	meanRem = rem + altRem
	if meanRem&0x80000000 != 0 || (meanRem == 0 && q&1 != 0) {
		rem = altRem
	}
	signRem = signA
	if rem >= 0x80000000 {
		signRem = !signRem
		rem = -rem
	}
	return Softfloat_normRoundPackToF32(signRem, expB, rem)
}
