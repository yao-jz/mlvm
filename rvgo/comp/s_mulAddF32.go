package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func Softfloat_mulAddF32(uiA uint32, uiB uint32, uiC uint32, op uint8) Float32_t {
	var signA, signB, signC, signProd, signZ bool
	var expA, expB, expC, expProd, expZ int16
	var sigA, sigB, sigC, magBits, sigZ uint32
	var sigProd uint64
	var expDiff int16
	var shiftDist int8
	var sig64Z, sig64C uint64
	var uiZ uint32

	/*------------------------------------------------------------------------
	 *------------------------------------------------------------------------*/
	signA = SignF32UI(uiA)
	expA = ExpF32UI(uiA)
	sigA = FracF32UI(uiA)
	signB = SignF32UI(uiB)
	expB = ExpF32UI(uiB)
	sigB = FracF32UI(uiB)
	signC = IntToBool(BoolToInt(SignF32UI(uiC)) ^ BoolToInt(op == Softfloat_mulAdd_subC))
	expC = ExpF32UI(uiC)
	sigC = FracF32UI(uiC)
	signProd = IntToBool(BoolToInt(signA) ^ BoolToInt(signB) ^ BoolToInt((op == Softfloat_mulAdd_subProd)))

	/*------------------------------------------------------------------------
	 *------------------------------------------------------------------------*/
	if expA == 0xFF {
		if (sigA != 0) || ((expB == 0xFF) && (sigB != 0)) {
			// propagateNaN_ABC
			uiZ = Softfloat_propagateNaNF32UI(uiA, uiB)
			// propagateNaN_ZC
			uiZ = Softfloat_propagateNaNF32UI(uiZ, uiC)
			// uiZ
			return Float32_t(uiZ)
		}
		magBits = uint32(expB) | sigB
		// infProdArg
		if magBits != 0 {
			uiZ = PackToF32UI(signProd, 0xFF, 0)
			if expC != 0xFF {
				// uiZ
				return Float32_t(uiZ)
			}
			if sigC != 0 {
				// propagateNaN_ZC
				uiZ = Softfloat_propagateNaNF32UI(uiZ, uiC)
				// uiZ
				return Float32_t(uiZ)
			}
			if signProd == signC {
				// uiZ
				return Float32_t(uiZ)
			}
		}
		// invalid: an infinite product with a zero factor, or an infinite product added
		// to an infinity of the opposite sign
		Softfloat_raiseFlags(Softfloat_flag_invalid)
		uiZ = 0x7FC00000
		// propagateNaN_ZC
		uiZ = Softfloat_propagateNaNF32UI(uiZ, uiC)
		// uiZ
		return Float32_t(uiZ)
	}
	if expB == 0xFF {
		if sigB != 0 {
			// propagateNaN_ABC
			uiZ = Softfloat_propagateNaNF32UI(uiA, uiB)
			// propagateNaN_ZC
			uiZ = Softfloat_propagateNaNF32UI(uiZ, uiC)
			// uiZ
			return Float32_t(uiZ)
		}
		magBits = uint32(expA) | sigA
		// infProdArg
		if magBits != 0 {
			uiZ = PackToF32UI(signProd, 0xFF, 0)
			if expC != 0xFF {
				// uiZ
				return Float32_t(uiZ)
			}
			if sigC != 0 {
				// propagateNaN_ZC
				uiZ = Softfloat_propagateNaNF32UI(uiZ, uiC)
				// uiZ
				return Float32_t(uiZ)
			}
			if signProd == signC {
				// uiZ
				return Float32_t(uiZ)
			}
		}
		// invalid: an infinite product with a zero factor, or an infinite product added
		// to an infinity of the opposite sign
		Softfloat_raiseFlags(Softfloat_flag_invalid)
		uiZ = 0x7FC00000
		// propagateNaN_ZC
		uiZ = Softfloat_propagateNaNF32UI(uiZ, uiC)
		// uiZ
		return Float32_t(uiZ)
	}
	if expC == 0xFF {
		if sigC != 0 {
			uiZ = 0
			// propagateNaN_ZC
			uiZ = Softfloat_propagateNaNF32UI(uiZ, uiC)
			// uiZ
			return Float32_t(uiZ)
		}
		uiZ = uiC
		// uiZ
		return Float32_t(uiZ)
	}

	/*------------------------------------------------------------------------
	 *------------------------------------------------------------------------*/

	if expA == 0 {
		if sigA == 0 {
			// zeroProd
			uiZ = uiC
			if !IntToBool(int(uint32(expC)|sigC)) && (signProd != signC) {
				uiZ = PackToF32UI(Softfloat_roundingMode == Softfloat_round_min, 0, 0)
			}
			// uiZ
			return Float32_t(uiZ)
		}
		expA, sigA = Softfloat_normSubnormalF32Sig(sigA)
	}
	if expB == 0 {
		if sigB == 0 {
			// zeroProd
			uiZ = uiC
			if !IntToBool(int(uint32(expC)|sigC)) && (signProd != signC) {
				uiZ = PackToF32UI(Softfloat_roundingMode == Softfloat_round_min, 0, 0)
			}
			// uiZ
			return Float32_t(uiZ)
		}
		expB, sigB = Softfloat_normSubnormalF32Sig(sigB)
	}

	/*------------------------------------------------------------------------
	 *------------------------------------------------------------------------*/

	expProd = expA + expB - 0x7E
	sigA = (sigA | 0x00800000) << 7
	sigB = (sigB | 0x00800000) << 7
	sigProd = uint64(sigA) * uint64(sigB)
	if sigProd < uint64(0x2000000000000000) {
		expProd -= 1
		sigProd <<= 1
	}
	signZ = signProd
	if expC == 0 {
		if sigC == 0 {
			expZ = expProd - 1
			sigZ = uint32(Softfloat_shortShiftRightJam64(sigProd, 31))
			// roundPack
			return Softfloat_roundPackToF32(signZ, int16(expZ), sigZ)
		}
		expC, sigC = Softfloat_normSubnormalF32Sig(sigC)
	}
	sigC = (sigC | 0x00800000) << 6
	expDiff = expProd - expC
	if signProd == signC {
		/*--------------------------------------------------------------------
		 *--------------------------------------------------------------------*/
		if expDiff <= 0 {
			expZ = expC
			sigZ = uint32(uint64(sigC) + Softfloat_shiftRightJam64(sigProd, uint32(32-expDiff)))
		} else {
			expZ = expProd
			sig64Z = sigProd + Softfloat_shiftRightJam64(uint64(sigC)<<32, uint32(expDiff))
			sigZ = uint32(Softfloat_shortShiftRightJam64(sig64Z, 32))
		}
		if sigZ < 0x40000000 {
			expZ -= 1
			sigZ <<= 1
		}
	} else {
		/*--------------------------------------------------------------------
		 *--------------------------------------------------------------------*/
		sig64C = uint64(sigC) << 32
		if expDiff < 0 {
			signZ = signC
			expZ = expC
			sig64Z = sig64C - Softfloat_shiftRightJam64(sigProd, uint32(-expDiff))
		} else if expDiff == 0 {
			expZ = expProd
			sig64Z = sigProd - sig64C
			if sig64Z == 0 {
				// completeCancellation
				uiZ = PackToF32UI(Softfloat_roundingMode == Softfloat_round_min, 0, 0)
				// uiZ
				return Float32_t(uiZ)
			}
			if (sig64Z & 0x8000000000000000) != 0 {
				signZ = !signZ
				sig64Z = -sig64Z
			}
		} else {
			expZ = expProd
			sig64Z = sigProd - Softfloat_shiftRightJam64(sig64C, uint32(expDiff))
		}
		shiftDist = int8(Softfloat_countLeadingZeros64(sig64Z) - 1)
		expZ -= int16(shiftDist)
		shiftDist -= 32
		if shiftDist < 0 {
			sigZ = uint32(Softfloat_shortShiftRightJam64(sig64Z, uint8(-shiftDist)))
		} else {
			sigZ = uint32(sig64Z) << uint32(shiftDist)
		}
	}
	// roundPack
	return Softfloat_roundPackToF32(signZ, int16(expZ), sigZ)
}
