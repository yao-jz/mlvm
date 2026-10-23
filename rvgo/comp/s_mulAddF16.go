package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func Softfloat_mulAddF16(uiA uint16, uiB uint16, uiC uint16, op uint8) Float16_t {
	var signA, signB, signC, signProd, signZ bool
	var expA, expB, expC, expProd, expZ int8
	var sigA, sigB, sigC, magBits, sigZ uint16
	var sigProd uint32
	var expDiff int8
	var shiftDist int8
	var sig32Z, sig32C uint32
	var uiZ uint16

	/*------------------------------------------------------------------------
	 *------------------------------------------------------------------------*/
	signA = SignF16UI(uiA)
	expA = ExpF16UI(uiA)
	sigA = FracF16UI(uiA)
	signB = SignF16UI(uiB)
	expB = ExpF16UI(uiB)
	sigB = FracF16UI(uiB)
	signC = IntToBool(BoolToInt(SignF16UI(uiC)) ^ BoolToInt(op == Softfloat_mulAdd_subC))
	expC = ExpF16UI(uiC)
	sigC = FracF16UI(uiC)
	signProd = IntToBool(BoolToInt(signA) ^ BoolToInt(signB) ^ BoolToInt((op == Softfloat_mulAdd_subProd)))

	/*------------------------------------------------------------------------
	 *------------------------------------------------------------------------*/
	if expA == 0x1F {
		if (sigA != 0) || ((expB == 0x1F) && (sigB != 0)) {
			// propagateNaN_ABC
			uiZ = Softfloat_propagateNaNF16UI(uiA, uiB)
			// propagateNaN_ZC
			uiZ = Softfloat_propagateNaNF16UI(uiZ, uiC)
			// uiZ
			return Float16_t(uiZ)
		}
		magBits = uint16(expB) | sigB
		// infProdArg
		if magBits != 0 {
			uiZ = PackToF16UI(signProd, 0x1F, 0)
			if expC != 0x1F {
				// uiZ
				return Float16_t(uiZ)
			}
			if sigC != 0 {
				// propagateNaN_ZC
				uiZ = Softfloat_propagateNaNF16UI(uiZ, uiC)
				// uiZ
				return Float16_t(uiZ)
			}
			if signProd == signC {
				// uiZ
				return Float16_t(uiZ)
			}
		}
		uiZ = 0x7E00
		// propagateNaN_ZC
		uiZ = Softfloat_propagateNaNF16UI(uiZ, uiC)
		// uiZ
		return Float16_t(uiZ)
	}
	if expB == 0x1F {
		if sigB != 0 {
			// propagateNaN_ABC
			uiZ = Softfloat_propagateNaNF16UI(uiA, uiB)
			// propagateNaN_ZC
			uiZ = Softfloat_propagateNaNF16UI(uiZ, uiC)
			// uiZ
			return Float16_t(uiZ)
		}
		magBits = uint16(expA) | sigA
		// infProdArg
		if magBits != 0 {
			uiZ = PackToF16UI(signProd, 0x1F, 0)
			if expC != 0x1F {
				// uiZ
				return Float16_t(uiZ)
			}
			if sigC != 0 {
				// propagateNaN_ZC
				uiZ = Softfloat_propagateNaNF16UI(uiZ, uiC)
				// uiZ
				return Float16_t(uiZ)
			}
			if signProd == signC {
				// uiZ
				return Float16_t(uiZ)
			}
		}
		uiZ = 0x7E00
		// propagateNaN_ZC
		uiZ = Softfloat_propagateNaNF16UI(uiZ, uiC)
		// uiZ
		return Float16_t(uiZ)
	}
	if expC == 0x1F {
		if sigC != 0 {
			uiZ = 0
			// propagateNaN_ZC
			uiZ = Softfloat_propagateNaNF16UI(uiZ, uiC)
			// uiZ
			return Float16_t(uiZ)
		}
		uiZ = uiC
		// uiZ
		return Float16_t(uiZ)
	}

	/*------------------------------------------------------------------------
	 *------------------------------------------------------------------------*/

	if expA == 0 {
		if sigA == 0 {
			// zeroProd
			uiZ = uiC
			if !IntToBool(int(uint16(expC)|sigC)) && (signProd != signC) {
				uiZ = PackToF16UI(Softfloat_roundingMode == Softfloat_round_min, 0, 0)
			}
			// uiZ
			return Float16_t(uiZ)
		}
		expA, sigA = Softfloat_normSubnormalF16Sig(sigA)
	}
	if expB == 0 {
		if sigB == 0 {
			// zeroProd
			uiZ = uiC
			if !IntToBool(int(uint16(expC)|sigC)) && (signProd != signC) {
				uiZ = PackToF16UI(Softfloat_roundingMode == Softfloat_round_min, 0, 0)
			}
			// uiZ
			return Float16_t(uiZ)
		}
		expB, sigB = Softfloat_normSubnormalF16Sig(sigB)
	}

	/*------------------------------------------------------------------------
	 *------------------------------------------------------------------------*/

	expProd = expA + expB - 0xE
	sigA = (sigA | 0x0400) << 4
	sigB = (sigB | 0x0400) << 4
	sigProd = uint32(sigA) * uint32(sigB)
	if sigProd < 0x20000000 {
		expProd -= 1
		sigProd <<= 1
	}
	signZ = signProd
	if expC == 0 {
		if sigC == 0 {
			expZ = expProd - 1
			sigZ = uint16(sigProd>>15 | uint32(BoolToInt((sigProd&0x7FFF) != 0)))
			// roundPack
			return Softfloat_roundPackToF16(signZ, int16(expZ), sigZ)
		}
		expC, sigC = Softfloat_normSubnormalF16Sig(sigC)
	}
	sigC = (sigC | 0x0400) << 3
	expDiff = expProd - expC
	if signProd == signC {
		/*--------------------------------------------------------------------
		 *--------------------------------------------------------------------*/
		if expDiff <= 0 {
			expZ = expC
			sigZ = uint16(uint32(sigC) + Softfloat_shiftRightJam32(sigProd, uint16(16-expDiff)))
		} else {
			expZ = expProd
			sig32Z = sigProd + Softfloat_shiftRightJam32(uint32(sigC)<<16, uint16(expDiff))
			sigZ = uint16(sig32Z>>16 | uint32(BoolToInt((sig32Z&0xFFFF) != 0)))
		}
		if sigZ < 0x4000 {
			expZ -= 1
			sigZ <<= 1
		}
	} else {
		/*--------------------------------------------------------------------
		 *--------------------------------------------------------------------*/
		sig32C = uint32(sigC) << 16
		if expDiff < 0 {
			signZ = signC
			expZ = expC
			sig32Z = sig32C - Softfloat_shiftRightJam32(sigProd, uint16(-expDiff))
		} else if expDiff == 0 {
			expZ = expProd
			sig32Z = sigProd - sig32C
			if sig32Z == 0 {
				// completeCancellation
				uiZ = PackToF16UI(Softfloat_roundingMode == Softfloat_round_min, 0, 0)
				// uiZ
				return Float16_t(uiZ)
			}
			if (sig32Z & 0x80000000) != 0 {
				signZ = !signZ
				sig32Z = -sig32Z
			}
		} else {
			expZ = expProd
			sig32Z = sigProd - Softfloat_shiftRightJam32(sig32C, uint16(expDiff))
		}
		shiftDist = int8(Softfloat_countLeadingZeros32(sig32Z) - 1)
		expZ -= shiftDist
		shiftDist -= 16
		if shiftDist < 0 {
			sigZ = uint16(sig32Z>>uint32((-shiftDist)) | uint32(BoolToInt(uint32(sig32Z<<uint32((shiftDist&31))) != 0)))
		} else {
			sigZ = uint16(sig32Z) << uint16(shiftDist)
		}
	}
	// roundPack
	return Softfloat_roundPackToF16(signZ, int16(expZ), sigZ)
}
