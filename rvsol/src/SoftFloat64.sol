// SPDX-License-Identifier: MIT
pragma solidity 0.8.15;

/// @title SoftFloat64
/// @notice IEEE-754 binary64 addition and subtraction in Yul, ported line-for-line from
///         `rvgo/comp`, which is itself a port of Berkeley SoftFloat 3e.
///
/// @dev This exists to answer one question with a number rather than an estimate: what does
///      it cost, in gas, to execute a floating-point RISC-V instruction on chain?
///
///      MLVM's off-chain VM implements RV64F/D through software IEEE-754, so a fraud proof
///      over a program that computes with floats needs the same semantics available to the
///      on-chain verifier. `RISCV.sol` currently reverts on float opcodes rather than
///      settling them. This library is the proof-of-concept for closing that gap: if the
///      gas cost is acceptable, the remaining work is mechanical porting of the other
///      operations (mul, div, sqrt, compare, convert).
///
///      Correctness is established by differential testing against the Go implementation,
///      not by inspection - see `test/SoftFloat64.t.sol` and
///      `scripts/softfloat-differential.sh`.
///
/// ## Porting notes
///
/// Three things differ between the Go source and this Yul, and every one of them is a
/// place where a bug would hide:
///
///  1. **Width.** Go's `uint64` wraps at 64 bits; Yul words are 256 bits and wrap there.
///     Every add and shift that could carry past bit 63 is followed by `mask64`.
///  2. **Shift argument order.** Yul is `shl(shiftAmount, value)`, the reverse of C and Go.
///  3. **Signed exponents.** Go uses `int16` for exponents and relies on `uint16(ex)`
///     reinterpretation to fold the `ex < 0` and `ex > 0x7FD` cases into one unsigned
///     comparison. Exponents here are carried as 16-bit two's-complement in a 256-bit word,
///     and `expLt`/`expNeg` do the sign handling explicitly.
library SoftFloat64 {
    /// @notice IEEE-754 binary64 addition. Operands and result are raw bit patterns.
    function add(uint64 a, uint64 b) external pure returns (uint64 z) {
        z = _add(a, b);
    }

    /// @notice IEEE-754 binary64 subtraction, as `a + (-b)`.
    function sub(uint64 a, uint64 b) external pure returns (uint64 z) {
        // Flipping the sign bit of b and adding is exactly what RISC-V FSUB.D does, and
        // what the Go implementation does.
        z = _add(a, uint64(b ^ 0x8000000000000000));
    }

    /// @notice Same as `add`, but callable internally so gas measurements can isolate the
    ///         arithmetic from the external-call overhead.
    function addInternal(uint64 a, uint64 b) public pure returns (uint64 z) {
        z = _add(a, b);
    }

    function _add(uint64 a, uint64 b) internal pure returns (uint64 z) {
        assembly {
            function mask64(v) -> out { out := and(v, 0xFFFFFFFFFFFFFFFF) }
            function signF64(v) -> out { out := shr(63, v) }
            function expF64(v) -> out { out := and(shr(52, v), 0x7FF) }
            function fracF64(v) -> out { out := and(v, 0x000FFFFFFFFFFFFF) }

            // PackToF64UI: (sign << 63) + (ex << 52) + sig, masked to 64 bits.
            function packF64(sign, ex, sig) -> out {
                out := mask64(add(add(shl(63, sign), shl(52, and(ex, 0x7FF))), sig))
            }

            // Exponents are 16-bit two's complement carried in a 256-bit word. `expSub`
            // keeps them in that representation so the sign tests below stay valid.
            function expSub(x, y) -> out { out := and(sub(x, y), 0xFFFF) }
            function expNeg(x) -> out { out := and(sub(0, x), 0xFFFF) }
            function expIsNeg(x) -> out { out := shr(15, and(x, 0xFFFF)) }

            // Berkeley SoftFloat's countLeadingZeros64, by binary search. Yul has no CLZ
            // opcode, so this is 6 conditional shifts - the same shape as the classic
            // bit-twiddling implementation.
            function clz64(x) -> out {
                switch x
                case 0 { out := 64 }
                default {
                    let n := 0
                    let v := mask64(x)
                    if iszero(shr(32, v)) { n := add(n, 32) v := mask64(shl(32, v)) }
                    if iszero(shr(48, v)) { n := add(n, 16) v := mask64(shl(16, v)) }
                    if iszero(shr(56, v)) { n := add(n, 8)  v := mask64(shl(8, v)) }
                    if iszero(shr(60, v)) { n := add(n, 4)  v := mask64(shl(4, v)) }
                    if iszero(shr(62, v)) { n := add(n, 2)  v := mask64(shl(2, v)) }
                    if iszero(shr(63, v)) { n := add(n, 1) }
                    out := n
                }
            }

            // Softfloat_shiftRightJam64: shift right, sticky-OR the bits shifted out.
            // Callers only ever pass dist >= 1, matching the Go source.
            function shiftRightJam64(v, dist) -> out {
                switch lt(dist, 63)
                case 1 {
                    let jam := 0
                    // Go: (a << (-dist & 63)) != 0, with -dist computed in unsigned 32-bit
                    if iszero(iszero(mask64(shl(and(sub(0, dist), 63), v)))) { jam := 1 }
                    out := or(shr(dist, v), jam)
                }
                default { if iszero(iszero(v)) { out := 1 } }
            }

            // Softfloat_roundPackToF64, round-to-nearest-even only. MLVM hardwires the
            // rounding mode, so roundIncrement is the constant 0x200 and the
            // roundIncrement == 0 branch of the original is unreachable.
            function roundPackToF64(sign, ex, sig) -> out {
                let roundBits := and(sig, 0x3FF)
                let e := and(ex, 0xFFFF)
                // Go: `if 0x7FD <= uint16(ex)`. Reinterpreting a negative int16 as uint16
                // makes it large, so this single unsigned test catches both ex < 0 and
                // ex > 0x7FD.
                if iszero(lt(e, 0x7FD)) {
                    switch expIsNeg(e)
                    case 1 {
                        sig := shiftRightJam64(sig, expNeg(e))
                        e := 0
                        roundBits := and(sig, 0x3FF)
                    }
                    default {
                        // 0x7FD < ex, or the rounding carry overflows into bit 63
                        if or(gt(e, 0x7FD), iszero(lt(mask64(add(sig, 0x200)), 0x8000000000000000))) {
                            out := packF64(sign, 0x7FF, 0)
                            leave
                        }
                    }
                }
                sig := shr(10, mask64(add(sig, 0x200)))
                // Ties-to-even: if the discarded bits were exactly half, clear the low bit.
                if iszero(xor(roundBits, 0x200)) { sig := and(sig, not(1)) }
                if iszero(sig) { e := 0 }
                out := packF64(sign, e, sig)
            }

            // Softfloat_normRoundPackToF64
            function normRoundPackToF64(sign, ex, sig) -> out {
                let shiftDist := sub(clz64(sig), 1)
                let e := expSub(ex, and(shiftDist, 0xFFFF))
                // Go: (10 <= shiftDist) && (uint64(ex) < 0x7FD). shiftDist is
                // non-negative here because clz64 >= 1 whenever this is reached.
                if and(iszero(lt(shiftDist, 10)), lt(e, 0x7FD)) {
                    let packedExp := e
                    if iszero(sig) { packedExp := 0 }
                    out := packF64(sign, packedExp, mask64(shl(sub(shiftDist, 10), sig)))
                    leave
                }
                out := roundPackToF64(sign, e, mask64(shl(shiftDist, sig)))
            }

            // Softfloat_addMagsF64
            function addMagsF64(uiA, uiB, signZ) -> out {
                let expA := expF64(uiA)
                let sigA := fracF64(uiA)
                let expB := expF64(uiB)
                let sigB := fracF64(uiB)
                let expDiff := expSub(expA, expB)
                let expZ := 0
                let sigZ := 0

                switch iszero(expDiff)
                case 1 {
                    if iszero(expA) { out := mask64(add(uiA, sigB)) leave }
                    if eq(expA, 0x7FF) {
                        if iszero(iszero(or(sigA, sigB))) { out := 0x7FF8000000000000 leave }
                        out := uiA
                        leave
                    }
                    expZ := expA
                    sigZ := mask64(shl(9, add(add(0x0020000000000000, sigA), sigB)))
                }
                default {
                    sigA := mask64(shl(9, sigA))
                    sigB := mask64(shl(9, sigB))
                    switch expIsNeg(expDiff)
                    case 1 {
                        if eq(expB, 0x7FF) {
                            if iszero(iszero(sigB)) { out := 0x7FF8000000000000 leave }
                            out := packF64(signZ, 0x7FF, 0)
                            leave
                        }
                        expZ := expB
                        switch iszero(expA)
                        case 0 { sigA := mask64(add(sigA, 0x2000000000000000)) }
                        default { sigA := mask64(shl(1, sigA)) }
                        sigA := shiftRightJam64(sigA, expNeg(expDiff))
                    }
                    default {
                        if eq(expA, 0x7FF) {
                            if iszero(iszero(sigA)) { out := 0x7FF8000000000000 leave }
                            out := uiA
                            leave
                        }
                        expZ := expA
                        switch iszero(expB)
                        case 0 { sigB := mask64(add(sigB, 0x2000000000000000)) }
                        default { sigB := mask64(shl(1, sigB)) }
                        sigB := shiftRightJam64(sigB, expDiff)
                    }
                    sigZ := mask64(add(add(0x2000000000000000, sigA), sigB))
                    if lt(sigZ, 0x4000000000000000) {
                        expZ := expSub(expZ, 1)
                        sigZ := mask64(shl(1, sigZ))
                    }
                }
                out := roundPackToF64(signZ, expZ, sigZ)
            }

            // Softfloat_subMagsF64, equal-exponent path.
            //
            // Split out of subMagsF64 because the two paths together need more than the 16
            // stack slots Yul can reach, and the alternative (compiling the whole contract
            // via-IR) would change the codegen for RISCV.sol and invalidate its gas
            // baseline. Splitting is also closer to how the original reads.
            function subMagsF64Equal(uiA, uiB, signZ, expA, sigA, sigB) -> out {
                if eq(expA, 0x7FF) {
                    // Both NaN and inf-minus-inf yield the canonical quiet NaN here.
                    out := 0x7FF8000000000000
                    leave
                }
                // Go computes sigDiff as int64 and tests its sign. sigA and sigB are both
                // below 2^52, so the difference fits and the sign test is a comparison.
                let sigDiff := 0
                let sign := signZ
                switch lt(sigA, sigB)
                case 1 { sigDiff := sub(sigB, sigA) sign := iszero(signZ) }
                default { sigDiff := sub(sigA, sigB) }
                if iszero(sigDiff) { out := 0 leave }
                let eA := expA
                if iszero(iszero(eA)) { eA := sub(eA, 1) }
                let shiftDist := sub(clz64(sigDiff), 11)
                let expZ := expSub(eA, and(shiftDist, 0xFFFF))
                if expIsNeg(expZ) {
                    shiftDist := eA
                    expZ := 0
                }
                out := packF64(sign, expZ, mask64(shl(shiftDist, sigDiff)))
            }

            // Softfloat_subMagsF64
            function subMagsF64(uiA, uiB, signZ) -> out {
                let expA := expF64(uiA)
                let sigA := fracF64(uiA)
                let expB := expF64(uiB)
                let sigB := fracF64(uiB)
                let expDiff := expSub(expA, expB)

                if iszero(expDiff) {
                    out := subMagsF64Equal(uiA, uiB, signZ, expA, sigA, sigB)
                    leave
                }

                sigA := mask64(shl(10, sigA))
                sigB := mask64(shl(10, sigB))
                let expZ := 0
                let sigZ := 0
                switch expIsNeg(expDiff)
                case 1 {
                    signZ := iszero(signZ)
                    if eq(expB, 0x7FF) {
                        if iszero(iszero(sigB)) { out := 0x7FF8000000000000 leave }
                        out := packF64(signZ, 0x7FF, 0)
                        leave
                    }
                    switch iszero(expA)
                    case 0 { sigA := mask64(add(sigA, 0x4000000000000000)) }
                    default { sigA := mask64(add(sigA, sigA)) }
                    sigA := shiftRightJam64(sigA, expNeg(expDiff))
                    sigB := or(sigB, 0x4000000000000000)
                    expZ := expB
                    sigZ := mask64(sub(sigB, sigA))
                }
                default {
                    if eq(expA, 0x7FF) {
                        if iszero(iszero(sigA)) { out := 0x7FF8000000000000 leave }
                        out := uiA
                        leave
                    }
                    switch iszero(expB)
                    case 0 { sigB := mask64(add(sigB, 0x4000000000000000)) }
                    default { sigB := mask64(add(sigB, sigB)) }
                    sigB := shiftRightJam64(sigB, expDiff)
                    sigA := or(sigA, 0x4000000000000000)
                    expZ := expA
                    sigZ := mask64(sub(sigA, sigB))
                }
                out := normRoundPackToF64(signZ, expSub(expZ, 1), sigZ)
            }

            // F64_add: same sign adds magnitudes, different sign subtracts them.
            let signA := signF64(a)
            let signB := signF64(b)
            switch eq(signA, signB)
            case 1 { z := addMagsF64(a, b, signA) }
            default { z := subMagsF64(a, b, signA) }
            z := mask64(z)
        }
    }
}
