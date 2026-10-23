package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F64_rem(a Float64_t, b Float64_t) Float64_t {
	var uiA, uiB, uiZ uint64
	var signA, signRem bool
	var expA, expB, expDiff int16
	var sigA, sigB, rem, altRem, meanRem uint64
	var q, recip32 uint32
	var q64 uint64

	uiA = uint64(a)
	signA = SignF64UI(uiA)
	expA = ExpF64UI(uiA)
	sigA = FracF64UI(uiA)
	uiB = uint64(b)
	expB = ExpF64UI(uiB)
	sigB = FracF64UI(uiB)

	if expA == 0x7FF {
		// fmt.Println("expA == 0x7FF")
		if sigA != 0 || (expB == 0x7FF && sigB != 0) {
			// propagateNaN
			uiZ = Softfloat_propagateNaNF64UI(uiA, uiB)
			// uiZ
			return Float64_t(uiZ)
		}
		// invalid
		uiZ = 0x7FF8000000000000
		// uiZ
		return Float64_t(uiZ)
	}
	if expB == 0x7FF {
		// fmt.Println("expB == 0x7FF")
		if sigB != 0 {
			// propagateNaN
			uiZ = Softfloat_propagateNaNF64UI(uiA, uiB)
			// uiZ
			return Float64_t(uiZ)
		}
		return a
	}

	if expA < expB-1 {
		// fmt.Println("expA < expB-1 ", a)
		return a
	}

	if expB == 0 {
		// fmt.Println("expB == 0")
		if sigB == 0 {
			// invalid
			uiZ = 0x7FF8000000000000
			// uiZ
			return Float64_t(uiZ)
		}
		expB, sigB = Softfloat_normSubnormalF64Sig(sigB)
	}
	if expA == 0 {
		// fmt.Println("expA == 0")
		if sigA == 0 {
			return a
		}
		expA, sigA = Softfloat_normSubnormalF64Sig(sigA)
	}

	rem = sigA | 0x0010000000000000
	sigB |= 0x0010000000000000
	expDiff = expA - expB
	if expDiff < 1 {
		// fmt.Println("expDiff < 1")
		if expDiff < -1 {
			return a
		}
		sigB <<= 9
		if expDiff != 0 {
			rem <<= 8
			q = 0
		} else {
			rem <<= 9
			q = 0
			if sigB <= rem {
				q = 1
				rem -= sigB
			}
		}
	} else {
		// fmt.Println("expDiff >= 1")
		recip32 = Softfloat_approxRecip32_1(uint32(sigB >> 21))
		rem <<= 9
		expDiff -= 30
		sigB <<= 9
		for {
			q64 = uint64(uint32(rem>>32)) * uint64(recip32)
			if expDiff < 0 {
				break
			}
			q = uint32((q64 + 0x80000000) >> 32)
			rem = uint64(uint32(rem>>3)) << 32
			rem -= uint64(q) * sigB
			if rem&0x8000000000000000 != 0 {
				rem += sigB
			}
			expDiff -= 29
		}
		q = uint32(q64>>32) >> uint32((^expDiff)&31)
		rem = (rem << (expDiff + 30)) - uint64(q)*sigB
		if rem&0x8000000000000000 != 0 {
			// fmt.Println("rem&0x8000000000000000 != 0")
			altRem = rem + sigB
			// selectRem
			meanRem = rem + altRem
			if (meanRem&0x8000000000000000) != 0 || (meanRem == 0 && (q&1) != 0) {
				rem = altRem
			}
			signRem = signA
			if rem&0x8000000000000000 != 0 {
				signRem = !signRem
				rem = -rem
			}
			return Softfloat_normRoundPackToF64(signRem, expB, rem)
		}
	}
	// fmt.Println("here")
	for {
		altRem = rem
		q++
		rem -= sigB
		if rem&0x8000000000000000 != 0 {
			break
		}
	}

	meanRem = rem + altRem
	if (meanRem&0x8000000000000000) != 0 || (meanRem == 0 && (q&1) != 0) {
		// fmt.Println("meanRem&0x8000000000000000 != 0 || (meanRem == 0 && (q&1) != 0)")
		rem = altRem
	}
	signRem = signA
	if rem&0x8000000000000000 != 0 {
		// fmt.Println("rem&0x8000000000000000 != 0")
		signRem = !signRem
		rem = -rem
	}
	return Softfloat_normRoundPackToF64(signRem, expB, rem)
}
