package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F64_mul(a Float64_t, b Float64_t) Float64_t {
	var uiA, uiB, uiZ uint64
	var signA, signB, signZ bool
	var expA, expB, expZ int16
	var sigA, sigB, sigZ uint64
	var sig128Z [4]uint32
	var magBits uint64

	uiA = uint64(a)
	signA = SignF64UI(uiA)
	expA = ExpF64UI(uiA)
	sigA = FracF64UI(uiA)
	uiB = uint64(b)
	signB = SignF64UI(uiB)
	expB = ExpF64UI(uiB)
	sigB = FracF64UI(uiB)
	signZ = signA != signB

	if expA == 0x7FF {
		if sigA != 0 || (expB == 0x7FF && sigB != 0) {
			// propagateNaN
			uiZ = Softfloat_propagateNaNF64UI(uiA, uiB)
			// uiZ
			return Float64_t(uiZ)
		}
		magBits = uint64(expB) | sigB
		// infArg
		if magBits == 0 {
			// invalid: zero times infinity
			Softfloat_raiseFlags(Softfloat_flag_invalid)
			uiZ = 0x7FF8000000000000
		} else {
			uiZ = PackToF64UI(signZ, 0x7FF, 0)
		}
		// uiZ
		return Float64_t(uiZ)
	}
	if expB == 0x7FF {
		if sigB != 0 {
			// propagateNaN
			uiZ = Softfloat_propagateNaNF64UI(uiA, uiB)
			// uiZ
			return Float64_t(uiZ)
		}
		magBits = uint64(expA) | sigA
		// infArg
		if magBits == 0 {
			// invalid: zero times infinity
			Softfloat_raiseFlags(Softfloat_flag_invalid)
			uiZ = 0x7FF8000000000000
		} else {
			uiZ = PackToF64UI(signZ, 0x7FF, 0)
		}
		// uiZ
		return Float64_t(uiZ)
	}

	if expA == 0 {
		if sigA == 0 {
			// zero
			uiZ = PackToF64UI(signZ, 0, 0)
			// uiZ
			return Float64_t(uiZ)
		}
		expA, sigA = Softfloat_normSubnormalF64Sig(sigA)
	}
	if expB == 0 {
		if sigB == 0 {
			// zero
			uiZ = PackToF64UI(signZ, 0, 0)
			// uiZ
			return Float64_t(uiZ)
		}
		expB, sigB = Softfloat_normSubnormalF64Sig(sigB)
	}

	expZ = expA + expB - 0x3FF
	sigA = (sigA | 0x0010000000000000) << 10
	sigB = (sigB | 0x0010000000000000) << 11
	// fmt.Println("here")
	Softfloat_mul64To128M(sigA, sigB, sig128Z[:])
	sigZ = uint64(sig128Z[IndexWord(4, 3)])<<32 | uint64(sig128Z[IndexWord(4, 2)])
	if sig128Z[IndexWord(4, 1)] != 0 || sig128Z[IndexWord(4, 0)] != 0 {
		sigZ |= 1
	}
	if sigZ < 0x4000000000000000 {
		expZ--
		sigZ <<= 1
	}
	return Softfloat_roundPackToF64(signZ, expZ, sigZ)
}
