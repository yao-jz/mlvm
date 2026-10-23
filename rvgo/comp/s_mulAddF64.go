package comp

import (
	. "github.com/yao-jz/mlvm/rvgo/include"
	. "github.com/yao-jz/mlvm/rvgo/riscv"
)

func Softfloat_mulAddF64(uiA uint64, uiB uint64, uiC uint64, op uint8) Float64_t {
	var signA, signB, signC, signZ bool
	var expA, expB, expC, expZ int16
	var sigA, sigB, sigC, magBits, sigZ uint64
	var expDiff int16
	var shiftDist int16
	var sig128Z [4]uint32
	var sig128C [4]uint32
	var uiZ uint64

	signA = SignF64UI(uiA)
	expA = ExpF64UI(uiA)
	sigA = FracF64UI(uiA)
	signB = SignF64UI(uiB)
	expB = ExpF64UI(uiB)
	sigB = FracF64UI(uiB)
	signC = IntToBool(BoolToInt(SignF64UI(uiC)) ^ BoolToInt(op == Softfloat_mulAdd_subC))
	expC = ExpF64UI(uiC)
	sigC = FracF64UI(uiC)
	signZ = IntToBool(BoolToInt(signA) ^ BoolToInt(signB) ^ BoolToInt((op == Softfloat_mulAdd_subProd)))

	if expA == 0x7FF {
		if (sigA != 0) || ((expB == 0x7FF) && (sigB != 0)) {
			// propagateNaN_ABC
			uiZ = Softfloat_propagateNaNF64UI(uiA, uiB)
			// propagateNaN_ZC
			uiZ = Softfloat_propagateNaNF64UI(uiZ, uiC)
			// uiZ
			return Float64_t(uiZ)
		}
		magBits = uint64(expB) | sigB
		// infProdArg
		if magBits != 0 {
			uiZ = PackToF64UI(signZ, 0x7FF, 0)
			if expC != 0x7FF {
				// uiZ
				return Float64_t(uiZ)
			}
			if sigC != 0 {
				// propagateNaN_ZC
				uiZ = Softfloat_propagateNaNF64UI(uiZ, uiC)
				// uiZ
				return Float64_t(uiZ)
			}
			if signZ == signC {
				// uiZ
				return Float64_t(uiZ)
			}
		}
		// invalid: an infinite product with a zero factor, or an infinite product added
		// to an infinity of the opposite sign
		Softfloat_raiseFlags(Softfloat_flag_invalid)
		uiZ = uint64(0x7FF8000000000000)
		// propagateNaN_ZC
		uiZ = Softfloat_propagateNaNF64UI(uiZ, uiC)
		// uiZ
		return Float64_t(uiZ)
	}
	if expB == 0x7FF {
		if sigB != 0 {
			// propagateNaN_ABC
			uiZ = Softfloat_propagateNaNF64UI(uiA, uiB)
			// propagateNaN_ZC
			uiZ = Softfloat_propagateNaNF64UI(uiZ, uiC)
			// uiZ
			return Float64_t(uiZ)
		}
		magBits = uint64(expA) | sigA
		// infProdArg
		if magBits != 0 {
			uiZ = PackToF64UI(signZ, 0x7FF, 0)
			if expC != 0x7FF {
				// uiZ
				return Float64_t(uiZ)
			}
			if sigC != 0 {
				// propagateNaN_ZC
				uiZ = Softfloat_propagateNaNF64UI(uiZ, uiC)
				// uiZ
				return Float64_t(uiZ)
			}
			if signZ == signC {
				// uiZ
				return Float64_t(uiZ)
			}
		}
		// invalid: an infinite product with a zero factor, or an infinite product added
		// to an infinity of the opposite sign
		Softfloat_raiseFlags(Softfloat_flag_invalid)
		uiZ = uint64(0x7FF8000000000000)
		// propagateNaN_ZC
		uiZ = Softfloat_propagateNaNF64UI(uiZ, uiC)
		// uiZ
		return Float64_t(uiZ)
	}
	if expC == 0x7FF {
		if sigC != 0 {
			uiZ = 0
			// propagateNaN_ZC
			uiZ = Softfloat_propagateNaNF64UI(uiZ, uiC)
			// uiZ
			return Float64_t(uiZ)
		}
		uiZ = uiC
		// uiZ
		return Float64_t(uiZ)
	}

	if expA == 0 {
		if sigA == 0 {
			// zeroProd
			uiZ = uiC
			if ((uint64(expC) | sigC) == 0) && (signZ != signC) {
				// completeCancellation
				uiZ = PackToF64UI(Softfloat_roundingMode == Softfloat_round_min, 0, 0)
			}
			// uiZ
			return Float64_t(uiZ)
		}
		expA, sigA = Softfloat_normSubnormalF64Sig(sigA)
	}
	if expB == 0 {
		if sigB == 0 {
			// zeroProd
			uiZ = uiC
			if ((uint64(expC) | sigC) == 0) && (signZ != signC) {
				// completeCancellation
				uiZ = PackToF64UI(Softfloat_roundingMode == Softfloat_round_min, 0, 0)
			}
			// uiZ
			return Float64_t(uiZ)
		}
		expB, sigB = Softfloat_normSubnormalF64Sig(sigB)
	}

	expZ = expA + expB - 0x3FE
	sigA = (sigA | uint64(0x0010000000000000)) << 10
	sigB = (sigB | uint64(0x0010000000000000)) << 11
	// fmt.Println("here")
	Softfloat_mul64To128M(sigA, sigB, sig128Z[:])
	sigZ = uint64(sig128Z[IndexWord(4, 3)])<<32 | uint64(sig128Z[IndexWord(4, 2)])
	shiftDist = 0
	if (sigZ & uint64(0x4000000000000000)) == 0 {
		expZ -= 1
		shiftDist = -1
	}
	if expC == 0 {
		if sigC == 0 {
			if shiftDist != 0 {
				sigZ <<= 1
			}
			// sigZ
			if IntToBool(int(sig128Z[IndexWord(4, 1)])) || IntToBool(int(sig128Z[IndexWord(4, 0)])) {
				sigZ |= 1
			}
			// roundPack
			return Softfloat_roundPackToF64(signZ, expZ-1, sigZ)
		}
		expC, sigC = Softfloat_normSubnormalF64Sig(sigC)
	}
	sigC = (sigC | uint64(0x0010000000000000)) << 10
	expDiff = expZ - expC
	if expDiff < 0 {
		expZ = expC
		if (signZ == signC) || (expDiff < -1) {
			shiftDist -= expDiff
			if shiftDist != 0 {
				sigZ = Softfloat_shiftRightJam64(sigZ, uint32(shiftDist))
			}
		} else {
			if shiftDist == 0 {
				Softfloat_shortShiftRightM(4, sig128Z[:], 1, sig128Z[:])
			}
		}
	} else {
		if shiftDist != 0 {
			Softfloat_addM(4, sig128Z[:], sig128Z[:], sig128Z[:])
		}
		if expDiff == 0 {
			sigZ = uint64(sig128Z[IndexWord(4, 3)])<<32 | uint64(sig128Z[IndexWord(4, 2)])
		} else {
			sig128C[IndexWord(4, 3)] = uint32(sigC >> 32)
			sig128C[IndexWord(4, 2)] = uint32(sigC)
			sig128C[IndexWord(4, 1)] = 0
			sig128C[IndexWord(4, 0)] = 0
			Softfloat_shiftRightJamM(4, sig128C[:], uint32(expDiff), sig128C[:])
		}
	}

	if signZ == signC {
		if expDiff <= 0 {
			sigZ += sigC
		} else {
			Softfloat_addM(4, sig128Z[:], sig128C[:], sig128Z[:])
			sigZ = uint64(sig128Z[IndexWord(4, 3)])<<32 | uint64(sig128Z[IndexWord(4, 2)])
		}
		if sigZ&0x8000000000000000 != 0 {
			expZ++
			sigZ = Softfloat_shortShiftRightJam64(sigZ, 1)
		}
	} else {
		if expDiff < 0 {
			signZ = signC
			if expDiff < -1 {
				sigZ = sigC - sigZ
				if sig128Z[IndexWord(4, 1)] != 0 || sig128Z[IndexWord(4, 0)] != 0 {
					sigZ = (sigZ - 1) | 1
				}
				if sigZ&0x4000000000000000 == 0 {
					expZ--
					sigZ <<= 1
				}
				// roundPack
				return Softfloat_roundPackToF64(signZ, expZ-1, sigZ)
			} else {
				sig128C[IndexWord(4, 3)] = uint32(sigC >> 32)
				sig128C[IndexWord(4, 2)] = uint32(sigC)
				sig128C[IndexWord(4, 1)] = 0
				sig128C[IndexWord(4, 0)] = 0
				Softfloat_subM(4, sig128C[:], sig128Z[:], sig128Z[:])
			}
		} else if expDiff == 0 {
			sigZ -= sigC
			if sigZ == 0 && sig128Z[IndexWord(4, 1)] == 0 && sig128Z[IndexWord(4, 0)] == 0 {
				// completeCancellation
				uiZ = PackToF64UI(Softfloat_roundingMode == Softfloat_round_min, 0, 0)
				// uiZ
				return Float64_t(uiZ)
			}
			sig128Z[IndexWord(4, 3)] = uint32(sigZ >> 32)
			sig128Z[IndexWord(4, 2)] = uint32(sigZ)
			if sigZ&0x8000000000000000 != 0 {
				signZ = !signZ
				Softfloat_negXM(4, sig128Z[:])
			}
		} else {
			Softfloat_subM(4, sig128Z[:], sig128C[:], sig128Z[:])
			if expDiff > 1 {
				sigZ = uint64(sig128Z[IndexWord(4, 3)])<<32 | uint64(sig128Z[IndexWord(4, 2)])
				if sigZ&0x4000000000000000 == 0 {
					expZ--
					sigZ <<= 1
				}
				// sigZ
				if sig128Z[IndexWord(4, 1)] != 0 || sig128Z[IndexWord(4, 0)] != 0 {
					sigZ |= 1
				}
				// roundPack
				return Softfloat_roundPackToF64(signZ, expZ-1, sigZ)
			}
		}
		shiftDist = 0
		sigZ = uint64(sig128Z[IndexWord(4, 3)])<<32 | uint64(sig128Z[IndexWord(4, 2)])
		if sigZ == 0 {
			shiftDist = 64
			sigZ = uint64(sig128Z[IndexWord(4, 1)])<<32 | uint64(sig128Z[IndexWord(4, 0)])
		}
		shiftDist += int16(Softfloat_countLeadingZeros64(sigZ) - 1)
		if shiftDist != 0 {
			expZ -= int16(shiftDist)
			Softfloat_shiftLeftM(4, sig128Z[:], uint32(shiftDist), sig128Z[:])
			sigZ = uint64(sig128Z[IndexWord(4, 3)])<<32 | uint64(sig128Z[IndexWord(4, 2)])
		}
	}
	// sigZ
	if sig128Z[IndexWord(4, 1)] != 0 || sig128Z[IndexWord(4, 0)] != 0 {
		sigZ |= 1
	}
	// roundPack
	return Softfloat_roundPackToF64(signZ, expZ-1, sigZ)
}
