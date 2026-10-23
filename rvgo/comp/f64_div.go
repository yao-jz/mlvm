package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func F64_div(a Float64_t, b Float64_t) Float64_t {
	var uiA, uiB, uiZ uint64
	var signA, signB, signZ bool
	var expA, expB, expZ int16
	var sigA, sigB, sigZ uint64
	var recip32, sig32Z, doubleTerm uint32
	var rem uint64
	var q uint32

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
		if sigA != 0 {
			// propagateNaN
			uiZ = Softfloat_propagateNaNF64UI(uiA, uiB)
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
			// invalid: infinity / infinity
			Softfloat_raiseFlags(Softfloat_flag_invalid)
			uiZ = 0x7FF8000000000000
			// uiZ
			return Float64_t(uiZ)
		}
		// infinity
		uiZ = PackToF64UI(signZ, 0x7FF, 0)
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
		// zero
		uiZ = PackToF64UI(signZ, 0, 0)
		// uiZ
		return Float64_t(uiZ)
	}

	if expB == 0 {
		if sigB == 0 {
			if uint64(expA)|sigA == 0 {
				// invalid: zero divided by zero
				Softfloat_raiseFlags(Softfloat_flag_invalid)
				uiZ = 0x7FF8000000000000
				// uiZ
				return Float64_t(uiZ)
			}
			// divide by zero: a finite non-zero numerator over zero. Berkeley calls this
			// flag "infinite"; RISC-V calls it DZ. Same bit, bit 3.
			Softfloat_raiseFlags(Softfloat_flag_infinite)
			uiZ = PackToF64UI(signZ, 0x7FF, 0)
			// uiZ
			return Float64_t(uiZ)
		}
		expB, sigB = Softfloat_normSubnormalF64Sig(sigB)
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

	expZ = expA - expB + 0x3FE
	sigA |= 0x0010000000000000
	sigB |= 0x0010000000000000
	if sigA < sigB {
		expZ--
		sigA <<= 11
	} else {
		sigA <<= 10
	}
	sigB <<= 11
	recip32 = Softfloat_approxRecip32_1(uint32(sigB>>32)) - 2
	sig32Z = uint32(uint64(uint32(sigA>>32)) * uint64(recip32) >> 32)
	doubleTerm = sig32Z << 1
	rem = ((sigA - uint64(doubleTerm)*uint64(sigB>>32)) << 28) - uint64(doubleTerm)*uint64(uint32(sigB)>>4)
	q = uint32(uint64(uint32(rem>>32))*uint64(recip32)>>32 + 4)
	sigZ = uint64(sig32Z)<<32 + uint64(q)<<4

	if (sigZ & 0x1FF) < 4<<4 {
		q &= ^uint32(7)
		sigZ &= ^uint64(0x7F)
		doubleTerm = q << 1
		// uint32(sigB)>>4, not uint64(sigB>>4): the C is `((uint32_t) sigB >> 4)`, which
		// truncates to the low 32 bits *before* shifting. Dropping the truncation lets the
		// high half of sigB into the product, which corrupts the remainder and rounds the
		// quotient the wrong way on about 2% of inputs - always by exactly one ULP, which is
		// why it survived until a differential test compared against a correctly-rounded
		// oracle. Line 102 above has the cast; this line had lost it.
		rem = ((rem - uint64(doubleTerm)*uint64(sigB>>32)) << 28) - uint64(doubleTerm)*uint64(uint32(sigB)>>4)
		if rem&0x8000000000000000 != 0 {
			sigZ -= 1 << 7
		} else {
			if rem != 0 {
				sigZ |= 1
			}
		}
	}
	return Softfloat_roundPackToF64(signZ, expZ, sigZ)
}
