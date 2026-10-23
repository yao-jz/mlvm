// SPDX-License-Identifier: MIT
pragma solidity ^0.8.13;

import {IPreimageOracle} from "@optimism/src/cannon/interfaces/IPreimageOracle.sol";

contract RISCV {

    IPreimageOracle public preimageOracle;

    constructor(IPreimageOracle _preimageOracle) {
        preimageOracle = _preimageOracle;
    }

    // Executes a single RISC-V instruction, starting from
    function step(bytes calldata stateData, bytes calldata proof, bytes32 localContext) public returns (bytes32) {
        assembly {
            function revertWithCode(code) {
                mstore(0, code)
                revert(0, 0x20)
            }

            function preimageOraclePos() -> out { // slot of preimageOraclePos field
                out := 0
            }

            //
            // Yul64 - functions to do 64 bit math - see yul64.go
            //
            function u64Mask() -> out { // max uint64
                out := shr(192, not(0)) // 256-64 = 192
            }

            function u32Mask() -> out {
                out := U64(shr(toU256(224), not(0))) // 256-32 = 224
            }

            function toU64(v) -> out {
                out := v
            }

            function shortToU64(v) -> out {
                out := v
            }

            function shortToU256(v) -> out {
                out := v
            }

            function longToU256(v) -> out {
                out := v
            }

            function u256ToU64(v) -> out {
                out := and(v, U256(u64Mask()))
            }

            function u64ToU256(v) -> out {
                out := v
            }

            function mask32Signed64(v) -> out {
                out := signExtend64(and64(v, u32Mask()), toU64(31))
            }

            function u64Mod() -> out { // 1 << 64
                out := shl(toU256(64), toU256(1))
            }

            function u64TopBit() -> out { // 1 << 63
                out := shl(toU256(63), toU256(1))
            }

            function signExtend64(v, bit) -> out {
                switch and(v, shl(bit, 1))
                case 0 {
                // fill with zeroes, by masking
                    out := U64(and(U256(v), shr(sub(toU256(63), bit), U256(u64Mask()))))
                }
                default {
                // fill with ones, by or-ing
                    out := U64(or(U256(v), shl(bit, shr(bit, U256(u64Mask())))))
                }
            }

            function signExtend64To256(v) -> out {
                switch and(U256(v), u64TopBit())
                case 0 {
                    out := v
                }
                default {
                    out := or(shl(toU256(64), not(0)), v)
                }
            }

            function add64(x, y) -> out {
                out := U64(mod(add(U256(x), U256(y)), u64Mod()))
            }

            function sub64(x, y) -> out {
                out := U64(mod(sub(U256(x), U256(y)), u64Mod()))
            }

            function mul64(x, y) -> out {
                out := u256ToU64(mul(U256(x), U256(y)))
            }

            function div64(x, y) -> out {
                out := u256ToU64(div(U256(x), U256(y)))
            }

            function sdiv64(x, y) -> out { // note: signed overflow semantics are the same between Go and EVM assembly
                out := u256ToU64(sdiv(signExtend64To256(x), signExtend64To256(y)))
            }

            function mod64(x, y) -> out {
                out := U64(mod(U256(x), U256(y)))
            }

            function smod64(x, y) -> out {
                out := u256ToU64(smod(signExtend64To256(x), signExtend64To256(y)))
            }

            function not64(x) -> out {
                out := u256ToU64(not(U256(x)))
            }

            function lt64(x, y) -> out {
                out := U64(lt(U256(x), U256(y)))
            }

            function gt64(x, y) -> out {
                out := U64(gt(U256(x), U256(y)))
            }

            function slt64(x, y) -> out {
                out := U64(slt(signExtend64To256(x), signExtend64To256(y)))
            }

            function sgt64(x, y) -> out {
                out := U64(sgt(signExtend64To256(x), signExtend64To256(y)))
            }

            function eq64(x, y) -> out {
                out := U64(eq(U256(x), U256(y)))
            }

            function iszero64(x) -> out {
                out := iszero(U256(x))
            }

            function and64(x, y) -> out {
                out := U64(and(U256(x), U256(y)))
            }

            function or64(x, y) -> out {
                out := U64(or(U256(x), U256(y)))
            }

            function xor64(x, y) -> out {
                out := U64(xor(U256(x), U256(y)))
            }

            function shl64(x, y) -> out {
                out := u256ToU64(shl(U256(x), U256(y)))
            }

            function shr64(x, y) -> out {
                out := U64(shr(U256(x), U256(y)))
            }

            function sar64(x, y) -> out {
                out := u256ToU64(sar(U256(x), signExtend64To256(y)))
            }

            // type casts, no-op in yul
            function b32asBEWord(v) -> out {
                out := v
            }
            function beWordAsB32(v) -> out {
                out := v
            }
            function U64(v) -> out {
                out := v
            }
            function U256(v) -> out {
                out := v
            }
            function toU256(v) -> out {
                out := v
            }

            //
            // Bit hacking util
            //
            function bitlen(x) -> n {
                if gt(x, sub(shl(128, 1), 1)) {
                    x := shr(128, x)
                    n := add(n, 128)
                }
                if gt(x, sub(shl(64, 1), 1)) {
                    x := shr(64, x)
                    n := add(n, 64)
                }
                if gt(x, sub(shl(32, 1), 1)) {
                    x := shr(32, x)
                    n := add(n, 32)
                }
                if gt(x, sub(shl(16, 1), 1)) {
                    x := shr(16, x)
                    n := add(n, 16)
                }
                if gt(x, sub(shl(8, 1), 1)) {
                    x := shr(8, x)
                    n := add(n, 8)
                }
                if gt(x, sub(shl(4, 1), 1)) {
                    x := shr(4, x)
                    n := add(n, 4)
                }
                if gt(x, sub(shl(2, 1), 1)) {
                    x := shr(2, x)
                    n := add(n, 2)
                }
                if gt(x, sub(shl(1, 1), 1)) {
                    x := shr(1, x)
                    n := add(n, 1)
                }
                if gt(x, 0) {
                    n := add(n, 1)
                }
            }

            function endianSwap(x) -> out {
                for { let i := 0 } lt(i, 32) { i := add(i, 1) } {
                    out := or(shl(8, out), and(x, 0xff))
                    x := shr(8, x)
                }
            }


            //
            // State layout
            //
            function stateSizeMemRoot()            -> out { out := 32 }
            function stateSizePreimageKey()        -> out { out := 32 }
            function stateSizePreimageOffset()     -> out { out := 8 }
            function stateSizePC()                 -> out { out := 8 }
            function stateSizeExitCode()           -> out { out := 1 }
            function stateSizeExited()             -> out { out := 1 }
            function stateSizeStep()               -> out { out := 8 }
            function stateSizeHeap()               -> out { out := 8 }
            function stateSizeLoadReservation()    -> out { out := 8 }
            function stateSizeRegisters()          -> out { out := mul(8, 32) }
            // The RV64F/D extension grew the witness beyond 362 bytes with float registers,
            // the getrandom(2) cursor, and fcsr. This contract does not execute F/D
            // instructions - a guest containing none does not need it to - but it must
            // parse and re-emit the full witness, or its post-state hash cannot match the
            // off-chain VM's. These fields pass through untouched.
            function stateSizeFRegisters()         -> out { out := mul(8, 32) }
            function stateSizeRandomFileAddr()     -> out { out := 8 }
            function stateSizeRandomFileOffset()   -> out { out := 8 }
            // fcsr. Unlike the float registers this is not merely passed through: the CSR
            // instructions that read and write it are implemented below, because a guest can
            // observe it even when it never executes float arithmetic.
            function stateSizeFCSR()               -> out { out := 8 }
            // Root of the authenticated staged-file index and descriptor tree.
            function stateSizeFileSystemRoot()     -> out { out := 32 }

            function stateOffsetMemRoot()          -> out { out := 0 }
            function stateOffsetPreimageKey()      -> out { out := add(stateOffsetMemRoot(), stateSizeMemRoot()) }
            function stateOffsetPreimageOffset()   -> out { out := add(stateOffsetPreimageKey(), stateSizePreimageKey()) }
            function stateOffsetPC()               -> out { out := add(stateOffsetPreimageOffset(), stateSizePreimageOffset()) }
            function stateOffsetExitCode()         -> out { out := add(stateOffsetPC(), stateSizePC()) }
            function stateOffsetExited()           -> out { out := add(stateOffsetExitCode(), stateSizeExitCode()) }
            function stateOffsetStep()             -> out { out := add(stateOffsetExited(), stateSizeExited()) }
            function stateOffsetHeap()             -> out { out := add(stateOffsetStep(), stateSizeStep()) }
            function stateOffsetLoadReservation()  -> out { out := add(stateOffsetHeap(), stateSizeHeap()) }
            function stateOffsetRegisters()        -> out { out := add(stateOffsetLoadReservation(), stateSizeLoadReservation()) }
            function stateOffsetFRegisters()       -> out { out := add(stateOffsetRegisters(), stateSizeRegisters()) }
            function stateOffsetRandomFileAddr()   -> out { out := add(stateOffsetFRegisters(), stateSizeFRegisters()) }
            function stateOffsetRandomFileOff()    -> out { out := add(stateOffsetRandomFileAddr(), stateSizeRandomFileAddr()) }
            function stateOffsetFCSR()             -> out { out := add(stateOffsetRandomFileOff(), stateSizeRandomFileOffset()) }
            function stateOffsetFileSystemRoot()   -> out { out := add(stateOffsetFCSR(), stateSizeFCSR()) }
            function stateSize()                   -> out { out := add(stateOffsetFileSystemRoot(), stateSizeFileSystemRoot()) }

            //
            // Initial EVM memory / calldata checks
            //
            if iszero(eq(mload(0x40), 0x80)) {
                // expected memory check: no allocated memory (start after scratch + free-mem-ptr + zero slot = 0x80)
                revert(0, 0)
            }
            if iszero(eq(stateData.offset, 132)) {
                // 32*4+4 = 132 expected state data offset
                revert(0, 0)
            }
            if iszero(eq(calldataload(sub(stateData.offset, 32)), stateSize())) {
                // user-provided state size must match expected state size
                revert(0, 0)
            }
            function paddedLen(v) -> out { // padded to multiple of 32 bytes
                let padding := mod(sub(32, mod(v, 32)), 32)
                out := add(v, padding)
            }
            if iszero(eq(proof.offset, add(add(stateData.offset, paddedLen(stateSize())), 32))) {
                // 132+stateSize+padding+32 = expected proof offset
                revert(0, 0)
            }
            function proofContentOffset() -> out { // since we can't reference proof.offset in functions, blame Yul
                // stateData.offset + paddedLen(stateSize) + 32, with stateData.offset
                // pinned to 132 by the check above. Derived rather than written as a
                // literal: it was hardcoded to 548 for the old 362-byte state, and silently
                // rejected proofs once the witness grew. Keep this derived from stateSize.
                out := add(add(132, paddedLen(stateSize())), 32)
            }
            if iszero(eq(proof.offset, proofContentOffset())) {
                revert(0, 0)
            }
            // TODO: validate abi offset values?

            //
            // State loading
            //
            function memStateOffset() -> out { out := 0x80 }
            // copy the state calldata into memory, so we can mutate it
            mstore(0x40, add(memStateOffset(), stateSize())) // alloc, update free mem pointer
            calldatacopy(memStateOffset(), stateData.offset, stateSize()) // same format in memory as in calldata

            //
            // State access
            //
            function readState(offset, length) -> out {
                // TODO revert if more than 32 bytes
                out := mload(add(memStateOffset(), offset)) // note: the state variables are all big-endian encoded
                out := shr(shl(3, sub(32, length)), out) // shift-right to right-align data and reduce to desired length
            }
            function writeState(offset, length, data) {
                // TODO revert if more than 32 bytes
                let memOffset := add(memStateOffset(), offset)
                // left-aligned mask of length bytes
                let mask := shl(shl(3, sub(32, length)), not(0))
                let prev := mload(memOffset)
                // align data to left
                data := shl(shl(3, sub(32, length)), data)
                // mask out data from previous word, and apply new data
                let result := or(and(prev, not(mask)), data)
                mstore(memOffset, result)
            }

            function getMemRoot() -> out {
                out := readState(stateOffsetMemRoot(), stateSizeMemRoot())
            }
            function setMemRoot(v) {
                writeState(stateOffsetMemRoot(), stateSizeMemRoot(), v)
            }

            function getFileSystemRoot() -> out {
                out := readState(stateOffsetFileSystemRoot(), stateSizeFileSystemRoot())
            }
            function setFileSystemRoot(v) {
                writeState(stateOffsetFileSystemRoot(), stateSizeFileSystemRoot(), v)
            }

            function getPreimageKey() -> out {
                out := readState(stateOffsetPreimageKey(), stateSizePreimageKey())
            }
            function setPreimageKey(k) {
                writeState(stateOffsetPreimageKey(), stateSizePreimageKey(), k)
            }

            function getPreimageOffset() -> out {
                out := readState(stateOffsetPreimageOffset(), stateSizePreimageOffset())
            }
            function setPreimageOffset(v) {
                writeState(stateOffsetPreimageOffset(), stateSizePreimageOffset(), v)
            }

            function getPC() -> out {
                out := readState(stateOffsetPC(), stateSizePC())
            }
            function setPC(v) {
                writeState(stateOffsetPC(), stateSizePC(), v)
            }

            function getExited() -> out {
                out := readState(stateOffsetExited(), stateSizeExited())
            }
            function setExited() {
                writeState(stateOffsetExited(), stateSizeExited(), 1)
            }

            function getExitCode() -> out {
                out := readState(stateOffsetExitCode(), stateSizeExitCode())
            }
            function setExitCode(v) {
                writeState(stateOffsetExitCode(), stateSizeExitCode(), v)
            }

            function getStep() -> out {
                out := readState(stateOffsetStep(), stateSizeStep())
            }
            function setStep(v) {
                writeState(stateOffsetStep(), stateSizeStep(), v)
            }

            function getHeap() -> out {
                out := readState(stateOffsetHeap(), stateSizeHeap())
            }
            function setHeap(v) {
                writeState(stateOffsetHeap(), stateSizeHeap(), v)
            }

            function getLoadReservation() -> out {
                out := readState(stateOffsetLoadReservation(), stateSizeLoadReservation())
            }
            function setLoadReservation(addr) {
                writeState(stateOffsetLoadReservation(), stateSizeLoadReservation(), addr)
            }

            function getRegister(reg) -> out {
                if gt64(reg, toU64(31)) {
                    revertWithCode(0xbad4e9) // cannot load invalid register
                }
                let offset := add64(toU64(stateOffsetRegisters()), mul64(reg, toU64(8)))
                out := readState(offset, 8)
            }
            function setRegister(reg, v) {
                if iszero64(reg) { // reg 0 must stay 0
                    // v is a HINT, but no hints are specified by standard spec, or used by us.
                    leave
                }
                if gt64(reg, toU64(31)) {
                    revertWithCode(0xbad4e9) // unknown register
                }
                let offset := add64(toU64(stateOffsetRegisters()), mul64(reg, toU64(8)))
                writeState(offset, 8, v)
            }

            // Float registers. This contract does not execute F/D arithmetic, but it does
            // execute the bit-manipulation subset - moves, sign injection, loads and
            // stores - so it needs to read and write these.
            function getFRegister(reg) -> out {
                if gt64(reg, toU64(31)) {
                    revertWithCode(0xbad4e9) // cannot load invalid Fregister
                }
                let offset := add64(stateOffsetFRegisters(), mul64(reg, toU64(8)))
                out := readState(offset, 8)
            }
            function setFRegister(reg, v) {
                if gt64(reg, toU64(31)) {
                    revertWithCode(0xbad4e9) // unknown Fregister
                }
                let offset := add64(stateOffsetFRegisters(), mul64(reg, toU64(8)))
                writeState(offset, 8, v)
            }

            //
            // State output
            //
            function vmStatus() -> status {
                switch getExited()
                case 1 {
                    switch getExitCode()
                    case 0 {
                        status := 0 // VMStatusValid
                    } case 1 {
                        status := 1 // VMStatusInvalid
                    } default {
                        status := 2 // VMStatusPanic
                    }
                } default {
                    status := 3 // VMStatusUnfinished
                }
            }

            function computeStateHash() -> out {
                // Log the RISC-V state for debugging
                log0(memStateOffset(), stateSize())

                out := keccak256(memStateOffset(), stateSize())
                out := or(and(not(shl(248, 0xFF)), out), shl(248, vmStatus()))
            }

            //
            // Parse - functions to parse RISC-V instructions - see parse.go
            //
            function parseImmTypeI(instr) -> out {
                out := signExtend64(shr64(toU64(20), instr), toU64(11))
            }

            function parseImmTypeS(instr) -> out {
                out := signExtend64(
                    or64(
                        shl64(toU64(5), shr64(toU64(25), instr)),
                        and64(shr64(toU64(7), instr), toU64(0x1F))
                    ),
                    toU64(11))
            }

            function parseImmTypeB(instr) -> out {
                out := signExtend64(
                    or64(
                        or64(
                            shl64(toU64(1), and64(shr64(toU64(8), instr), toU64(0xF))),
                            shl64(toU64(5), and64(shr64(toU64(25), instr), toU64(0x3F)))
                        ),
                        or64(
                            shl64(toU64(11), and64(shr64(toU64(7), instr), toU64(1))),
                            shl64(toU64(12), shr64(toU64(31), instr))
                        )
                    ),
                    toU64(12)
                )
            }

            function parseImmTypeU(instr) -> out {
                out := signExtend64(shr64(toU64(12), instr), toU64(19))
            }

            function parseImmTypeJ(instr) -> out {
                out := signExtend64(
                    or64(
                        or64(
                            and64(shr64(toU64(21), instr), shortToU64(0x3FF)),          // 10 bits for index 0:9
                            shl64(toU64(10), and64(shr64(toU64(20), instr), toU64(1)))  // 1 bit for index 10
                        ),
                        or64(
                            shl64(toU64(11), and64(shr64(toU64(12), instr), toU64(0xFF))), // 8 bits for index 11:18
                            shl64(toU64(19), shr64(toU64(31), instr))                      // 1 bit for index 19
                        )
                    ),
                    toU64(19)
                )
            }

            function parseOpcode(instr) -> out {
                out := and64(instr, toU64(0x7F))
            }

            function parseRd(instr) -> out {
                out := and64(shr64(toU64(7), instr), toU64(0x1F))
            }

            function parseFunct3(instr) -> out {
                out := and64(shr64(toU64(12), instr), toU64(0x7))
            }

            function parseRs1(instr) -> out {
                out := and64(shr64(toU64(15), instr), toU64(0x1F))
            }

            function parseRs2(instr) -> out {
                out := and64(shr64(toU64(20), instr), toU64(0x1F))
            }

            // Float-instruction fields. Must stay identical to fast/parse.go and
            // slow/parse.go field-for-field, or the three implementations decode
            // differently.
            function parseWidth(instr) -> out {
                out := and64(shr64(toU64(12), instr), toU64(0x7))
            }
            function parsefmt(instr) -> out {
                out := and64(shr64(toU64(25), instr), toU64(0x3))
            }
            function parseFunct5(instr) -> out {
                out := and64(shr64(toU64(27), instr), toU64(0x1F))
            }
            function parseFunct7(instr) -> out {
                out := shr64(toU64(25), instr)
            }

            function parseCSSR(instr) -> out {
                out := shr64(toU64(20), instr)
            }

            //
            // Memory functions
            //
            function proofOffset(proofIndex) -> offset {
                // proof size: 64-5+1=60 (a 64-bit mem-address branch to 32 byte leaf, incl leaf itself), all 32 bytes
                offset := mul64(mul64(toU64(proofIndex), toU64(60)), toU64(32))
                offset := add64(offset, proofContentOffset())
            }

            function hashPair(a, b) -> h {
                mstore(0, a)
                mstore(0x20, b)
                h := keccak256(0, 0x40)
            }

            function getMemoryB32(addr, proofIndex) -> out {
                if and64(addr, toU64(31)) { // quick addr alignment check
                    revertWithCode(0xbad10ad0) // addr not aligned with 32 bytes
                }
                let offset := proofOffset(proofIndex)
                let leaf := calldataload(offset)
                offset := add64(offset, toU64(32))

                let path := shr64(toU64(5), addr) // 32 bytes of memory per leaf
                let node := leaf                  // starting from the leaf node, work back up by combining with siblings, to reconstruct the root
                for { let i := 0 } lt(i, sub(64, 5)) { i := add(i, 1) } {
                    let sibling := calldataload(offset)
                    offset := add64(offset, toU64(32))
                    switch and64(shr64(toU64(i), path), toU64(1))
                    case 0 {
                        node := hashPair(node, sibling)
                    } case 1 {
                        node := hashPair(sibling, node)
                    }
                }
                let memRoot := getMemRoot()
                if iszero(eq(b32asBEWord(node), b32asBEWord(memRoot))) { // verify the root matches
                    revertWithCode(0xbadf00d1) // bad memory proof
                }
                out := leaf
            }

            // warning: setMemoryB32 does not verify the proof,
            // it assumes the same memory proof has been verified with getMemoryB32
            function setMemoryB32(addr, v, proofIndex) {
                if and64(addr, toU64(31)) {
                    revertWithCode(0xbad10ad0) // addr not aligned with 32 bytes
                }
                let offset := proofOffset(proofIndex)
                let leaf := v
                offset := add64(offset, toU64(32))
                let path := shr64(toU64(5), addr) // 32 bytes of memory per leaf
                let node := leaf                  // starting from the leaf node, work back up by combining with siblings, to reconstruct the root
                for { let i := 0 } lt(i, sub(64, 5)) { i := add(i, 1) } {
                    let sibling := calldataload(offset)
                    offset := add64(offset, toU64(32))

                    switch and64(shr64(toU64(i), path), toU64(1))
                    case 0 {
                        node := hashPair(node, sibling)
                    } case 1 {
                        node := hashPair(sibling, node)
                    }
                }
                setMemRoot(node) // store new memRoot
            }

            // load unaligned, optionally signed, little-endian, integer of 1 ... 8 bytes from memory
            function loadMem(addr, size, signed, proofIndexL, proofIndexR) -> out {
                if gt(size, 8) {
                    revertWithCode(0xbad512e0) // cannot load more than 8 bytes
                }
                // load/verify left part
                let leftAddr := and64(addr, not64(toU64(31)))
                let left := b32asBEWord(getMemoryB32(leftAddr, proofIndexL))
                let alignment := sub64(addr, leftAddr)

                let right := 0
                let rightAddr := and64(add64(addr, sub64(size, toU64(1))), not64(toU64(31)))
                let leftShamt := sub64(sub64(toU64(32), alignment), size)
                let rightShamt := toU64(0)
                if iszero64(eq64(leftAddr, rightAddr)) {
                    // if unaligned, use second proof for the right part
                    if eq(proofIndexR, 0xff) {
                        revertWithCode(0xbad22220) // unexpected need for right-side proof in loadMem
                    }
                    // load/verify right part
                    right := b32asBEWord(getMemoryB32(rightAddr, proofIndexR))
                    // left content is aligned to right of 32 bytes
                    leftShamt := toU64(0)
                    rightShamt := sub64(sub64(toU64(64), alignment), size)
                }

                // left: prepare for byte-taking by right-aligning
                left := shr(u64ToU256(shl64(toU64(3), leftShamt)), left)
                // right: right-align for byte-taking by right-aligning
                right := shr(u64ToU256(shl64(toU64(3), rightShamt)), right)
                // loop:
                for { let i := 0 } lt(i, size) { i := add(i, 1) } {
                    // translate to reverse byte lookup, since we are reading little-endian memory, and need the highest byte first.
                    // effAddr := (addr + size - 1 - i) &^ 31
                    let effAddr := and64(sub64(sub64(add64(addr, size), toU64(1)), toU64(i)), not64(toU64(31)))
                    // take a byte from either left or right, depending on the effective address
                    let b := toU256(0)
                    switch eq64(effAddr, leftAddr)
                    case 1 {
                        b := and(left, toU256(0xff))
                        left := shr(toU256(8), left)
                    } case 0 {
                        b := and(right, toU256(0xff))
                        right := shr(toU256(8), right)
                    }
                    // append it to the output
                    out := or64(shl64(toU64(8), out), u256ToU64(b))
                }

                if signed {
                    let signBitShift := sub64(shl64(toU64(3), size), toU64(1))
                    out := signExtend64(out, signBitShift)
                }
            }

            // Splits the value into a left and a right part, each with a mask (identify data) and a patch (diff content).
            function leftAndRight(alignment, size, value) -> leftMask, rightMask, leftPatch, rightPatch {
                let start := alignment
                let end := add64(alignment, size)
                for { let i := 0 } lt(i, 64) { i := add(i, 1) } {
                    let index := toU64(i)
                    let leftSide := lt64(index, toU64(32))
                    switch leftSide
                    case 1 {
                        leftPatch := shl(8, leftPatch)
                        leftMask := shl(8, leftMask)
                    } case 0 {
                        rightPatch := shl(8, rightPatch)
                        rightMask := shl(8, rightMask)
                    }
                    if and64(eq64(lt64(index, start), toU64(0)), lt64(index, end)) { // if alignment <= i < alignment+size
                        let b := and(shr(u64ToU256(shl64(toU64(3), sub64(index, alignment))), value), toU256(0xff))
                        switch leftSide
                        case 1 {
                            leftPatch := or(leftPatch, b)
                            leftMask := or(leftMask, toU256(0xff))
                        } case 0 {
                            rightPatch := or(rightPatch, b)
                            rightMask := or(rightMask, toU256(0xff))
                        }
                    }
                }
            }

            function storeMemUnaligned(addr, size, value, proofIndexL, proofIndexR) {
                if gt(size, 32) {
                    revertWithCode(0xbad512e1) // cannot store more than 32 bytes
                }

                let leftAddr := and64(addr, not64(toU64(31)))
                let rightAddr := and64(add64(addr, sub64(size, toU64(1))), not64(toU64(31)))
                let alignment := sub64(addr, leftAddr)
                let leftMask, rightMask, leftPatch, rightPatch := leftAndRight(alignment, size, value)

                // load the left base
                let left := b32asBEWord(getMemoryB32(leftAddr, proofIndexL))
                // apply the left patch
                left := or(and(left, not(leftMask)), leftPatch)
                // write the left
                setMemoryB32(leftAddr, beWordAsB32(left), proofIndexL)

                // if aligned: nothing more to do here
                if eq64(leftAddr, rightAddr) {
                    leave
                }
                if eq(proofIndexR, 0xff) {
                    revertWithCode(0xbad22221) // unexpected need for right-side proof in storeMem
                }
                // load the right base (with updated mem root)
                let right := b32asBEWord(getMemoryB32(rightAddr, proofIndexR))
                // apply the right patch
                right := or(and(right, not(rightMask)), rightPatch)
                // write the right (with updated mem root)
                setMemoryB32(rightAddr, beWordAsB32(right), proofIndexR)
            }

            function storeMem(addr, size, value, proofIndexL, proofIndexR) {
                storeMemUnaligned(addr, size, u64ToU256(value), proofIndexL, proofIndexR)
            }

            // Store one 16-byte chunk and return the next sequential proof index.
            // A chunk may straddle two 32-byte memory leaves at an arbitrary guest address.
            function storeSysinfoChunk(addr, offset, value, proofIndex) -> nextProofIndex {
                let chunkAddr := add64(addr, offset)
                let leftAddr := and64(chunkAddr, not64(toU64(31)))
                let rightAddr := and64(add64(chunkAddr, toU64(15)), not64(toU64(31)))
                storeMemUnaligned(chunkAddr, toU64(16), value, proofIndex, add(proofIndex, 1))
                nextProofIndex := add(proofIndex, 1)
                if iszero(eq64(leftAddr, rightAddr)) {
                    nextProofIndex := add(nextProofIndex, 1)
                }
            }

            //
            // Authenticated staged-file metadata
            //
            // The fixed 31-word trailer is appended after all memory proofs:
            // header[5], file[4], file siblings[10], descriptor[2], descriptor siblings[10].
            function fileSystemProofSize() -> out { out := 992 }
            function fileSystemProofOffset() -> out {
                let proofLength := calldataload(sub(proofContentOffset(), 32))
                if lt(proofLength, fileSystemProofSize()) { revertWithCode(0xbadf11e0) }
                let proofEnd := add(proofContentOffset(), proofLength)
                if or(lt(proofEnd, proofContentOffset()), gt(proofEnd, calldatasize())) {
                    revertWithCode(0xbadf11e0)
                }
                out := sub(proofEnd, fileSystemProofSize())
            }

            function fileSystemProofWord(index) -> out {
                out := calldataload(add(fileSystemProofOffset(), mul(index, 32)))
            }

            function fileSystemProofU64(index) -> out {
                out := fileSystemProofWord(index)
                if shr(64, out) { revertWithCode(0xbadf11e1) }
            }

            function fixedBranchRoot(leaf, index, siblingWord) -> node {
                node := leaf
                for { let depth := 0 } lt(depth, 10) { depth := add(depth, 1) } {
                    let sibling := fileSystemProofWord(add(siblingWord, depth))
                    switch and(shr(depth, index), 1)
                    case 0 { node := hashPair(node, sibling) }
                    default { node := hashPair(sibling, node) }
                }
            }

            function stagedFileLeaf(fileID, fileSize, fileAddr) -> out {
                // keccak256("MLVM-FILE-V2\0" || id || uint64(size) || uint64(addr))
                mstore(0, shl(152, 0x4d4c564d2d46494c452d563200))
                mstore(13, fileID)
                mstore(45, shl(192, fileSize))
                mstore(53, shl(192, fileAddr))
                out := keccak256(0, 61)
            }

            function stagedDescriptorLeaf(fileID, offset) -> out {
                // keccak256("MLVM-FD-V2\0" || id || uint64(offset))
                mstore(0, shl(168, 0x4d4c564d2d46442d563200))
                mstore(11, fileID)
                mstore(43, shl(192, offset))
                out := keccak256(0, 51)
            }

            function computeFileSystemRoot(filePointer, nextFD, fileCount, fileRoot, descriptorRoot) -> out {
                // keccak256("MLVM-FILESYSTEM-V2\0" || three uint64 values || two roots)
                mstore(0, shl(104, 0x4d4c564d2d46494c4553595354454d2d563200))
                mstore(19, shl(192, filePointer))
                mstore(27, shl(192, nextFD))
                mstore(35, shl(192, fileCount))
                mstore(43, fileRoot)
                mstore(75, descriptorRoot)
                out := keccak256(0, 107)
            }

            function verifyFileSystemProof(fd) {
                if gt(fd, 1023) { revertWithCode(0xbadf11e2) }
                let filePointer := fileSystemProofU64(0)
                let nextFD := fileSystemProofU64(1)
                let fileCount := fileSystemProofU64(2)
                let fileRoot := fileSystemProofWord(3)
                let descriptorRoot := fileSystemProofWord(4)
                let fileIndex := fileSystemProofU64(5)
                let fileID := fileSystemProofWord(6)
                let fileSize := fileSystemProofU64(7)
                let fileAddr := fileSystemProofU64(8)

                if or(lt(nextFD, 100), gt(nextFD, 1024)) { revertWithCode(0xbadf11e3) }
                if gt(fileCount, 1024) { revertWithCode(0xbadf11e4) }
                if gt(fileIndex, 1023) { revertWithCode(0xbadf11e5) }

                let fileLeaf := 0
                switch iszero(fileID)
                case 1 {
                    if or(fileSize, fileAddr) { revertWithCode(0xbadf11e6) }
                    if lt(fileIndex, fileCount) { revertWithCode(0xbadf11e6) }
                }
                default {
                    if iszero(lt(fileIndex, fileCount)) { revertWithCode(0xbadf11e7) }
                    if gt(fileSize, shl(36, 1)) { revertWithCode(0xbadf11e8) }
                    if gt(add(fileAddr, fileSize), u64Mask()) { revertWithCode(0xbadf11e8) }
                    fileLeaf := stagedFileLeaf(fileID, fileSize, fileAddr)
                }
                if iszero(eq(fixedBranchRoot(fileLeaf, fileIndex, 9), fileRoot)) {
                    revertWithCode(0xbadf11e9)
                }

                let descriptorID := fileSystemProofWord(19)
                let descriptorOffset := fileSystemProofU64(20)
                let descriptorLeaf := 0
                switch iszero(descriptorID)
                case 1 {
                    if descriptorOffset { revertWithCode(0xbadf11ea) }
                }
                default { descriptorLeaf := stagedDescriptorLeaf(descriptorID, descriptorOffset) }
                if iszero(eq(fixedBranchRoot(descriptorLeaf, fd, 21), descriptorRoot)) {
                    revertWithCode(0xbadf11eb)
                }

                let root := computeFileSystemRoot(filePointer, nextFD, fileCount, fileRoot, descriptorRoot)
                if iszero(eq(root, getFileSystemRoot())) { revertWithCode(0xbadf11ec) }
            }

            function setUpdatedFileSystemRoot(fd, nextFD, fileID, offset) {
                if or(lt(nextFD, 100), gt(nextFD, 1024)) { revertWithCode(0xbadf11ed) }
                let leaf := 0
                switch iszero(fileID)
                case 1 {
                    if offset { revertWithCode(0xbadf11ee) }
                }
                default { leaf := stagedDescriptorLeaf(fileID, offset) }
                let descriptorRoot := fixedBranchRoot(leaf, fd, 21)
                let root := computeFileSystemRoot(
                    fileSystemProofU64(0),
                    nextFD,
                    fileSystemProofU64(2),
                    fileSystemProofWord(3),
                    descriptorRoot
                )
                setFileSystemRoot(root)
            }

            function validateExistingDescriptor() {
                let fileID := fileSystemProofWord(6)
                let fileSize := fileSystemProofU64(7)
                let descriptorID := fileSystemProofWord(19)
                let descriptorOffset := fileSystemProofU64(20)
                if iszero(descriptorID) { revertWithCode(0xbadf11ef) }
                if iszero(eq(descriptorID, fileID)) { revertWithCode(0xbadf11f0) }
                if gt(descriptorOffset, fileSize) { revertWithCode(0xbadf11f1) }
            }

            function isKernelPseudoFile(pathPtr, pathLen) -> out {
                let head := mload(pathPtr)
                if eq(pathLen, 5) {
                    if eq(shr(216, head), 0x2f70726f63) { out := 1 } // /proc
                }
                if eq(pathLen, 4) {
                    let bare := shr(224, head)
                    if or(or(eq(bare, 0x2f737973), eq(bare, 0x2f646576)), eq(bare, 0x2f72756e)) {
                        out := 1 // /sys, /dev, /run
                    }
                }
                if iszero(lt(pathLen, 6)) {
                    if eq(shr(208, head), 0x2f70726f632f) { out := 1 } // /proc/
                }
                if iszero(lt(pathLen, 5)) {
                    let prefix := shr(216, head)
                    if or(or(eq(prefix, 0x2f7379732f), eq(prefix, 0x2f6465762f)), eq(prefix, 0x2f72756e2f)) {
                        out := 1 // /sys/, /dev/, /run/
                    }
                }
            }

            function readStagedFileName(addr) -> pathHash, pseudo {
                let pathPtr := mload(0x40)
                mstore(0x40, add(pathPtr, 256))
                let pathLen := 0
                let leafAddr := and64(addr, not64(toU64(31)))
                let leafOffset := and64(addr, toU64(31))
                let proofIndex := 1
                for { } 1 { } {
                    let leaf := getMemoryB32(leafAddr, proofIndex)
                    proofIndex := add(proofIndex, 1)
                    for { let i := leafOffset } lt(i, 32) { i := add(i, 1) } {
                        let b := byte(i, leaf)
                        if iszero(b) {
                            pathHash := keccak256(pathPtr, pathLen)
                            pseudo := isKernelPseudoFile(pathPtr, pathLen)
                            leave
                        }
                        if eq(pathLen, 255) { revertWithCode(0xf001ca11) }
                        mstore8(add(pathPtr, pathLen), b)
                        pathLen := add(pathLen, 1)
                    }
                    leafAddr := add64(leafAddr, toU64(32))
                    leafOffset := 0
                }
            }

            function copyStagedFileBytes(fileAddr, fileSize, descriptorOffset, addr, count) -> n {
                n := count
                if gt(n, 32) { n := 32 }
                let remaining := sub(fileSize, descriptorOffset)
                if gt(n, remaining) { n := remaining }
                if iszero(n) { leave }

                let sourceAddr := add64(fileAddr, descriptorOffset)
                let sourceLeftAddr := and64(sourceAddr, not64(toU64(31)))
                let sourceLeft := getMemoryB32(sourceLeftAddr, 1)
                let sourceRight := 0
                let nextProofIndex := 2
                if iszero(eq(and64(add64(sourceAddr, sub64(n, 1)), not64(toU64(31))), sourceLeftAddr)) {
                    sourceRight := getMemoryB32(add64(sourceLeftAddr, 32), 2)
                    nextProofIndex := 3
                }

                let value := 0
                for { let i := 0 } lt(i, n) { i := add(i, 1) } {
                    let pos := add64(sourceAddr, i)
                    let b := 0
                    switch eq(and64(pos, not64(toU64(31))), sourceLeftAddr)
                    case 1 { b := byte(and(pos, 31), sourceLeft) }
                    default { b := byte(and(pos, 31), sourceRight) }
                    value := or(value, shl(mul(8, i), b))
                }
                storeMemUnaligned(addr, n, value, nextProofIndex, add(nextProofIndex, 1))
            }

            function readStagedFile(fd, addr, count) -> n, errCode {
                if gt(fd, 1023) {
                    n := u64Mask()
                    errCode := toU64(0x4d)
                    leave
                }
                verifyFileSystemProof(fd)
                let descriptorID := fileSystemProofWord(19)
                if iszero(descriptorID) {
                    n := u64Mask()
                    errCode := toU64(0x4d)
                    leave
                }
                validateExistingDescriptor()
                let descriptorOffset := fileSystemProofU64(20)
                n := copyStagedFileBytes(
                    fileSystemProofU64(8),
                    fileSystemProofU64(7),
                    descriptorOffset,
                    addr,
                    count
                )
                setUpdatedFileSystemRoot(fd, fileSystemProofU64(1), descriptorID, add64(descriptorOffset, n))
            }

            function stagedDescriptorExists(fd) -> exists {
                if gt(fd, 1023) { leave }
                verifyFileSystemProof(fd)
                if iszero(fileSystemProofWord(19)) { leave }
                validateExistingDescriptor()
                exists := 1
            }

            function openStagedFile(pathname) -> out, errCode {
                let pathHash, pseudo := readStagedFileName(pathname)
                if pseudo {
                    out := u64Mask()
                    errCode := toU64(0xd)
                    leave
                }
                let fd := fileSystemProofU64(1)
                if gt(fd, 1023) { revertWithCode(0xbadf11f2) }
                verifyFileSystemProof(fd)
                if or(fileSystemProofWord(19), fileSystemProofU64(20)) { revertWithCode(0xbadf11f3) }
                let fileID := fileSystemProofWord(6)
                if iszero(fileID) { revertWithCode(0xbadf11f4) }
                if iszero(eq(fileID, pathHash)) { revertWithCode(0xbadf11f5) }
                setUpdatedFileSystemRoot(fd, add64(fd, 1), fileID, 0)
                out := fd
            }

            function closeStagedFile(fd) {
                if gt(fd, 1023) { revertWithCode(0xf001ca11) }
                verifyFileSystemProof(fd)
                validateExistingDescriptor()
                setUpdatedFileSystemRoot(fd, fileSystemProofU64(1), 0, 0)
            }

            function seekStagedFile(fd, offset, whence) -> out, errCode {
                if gt(fd, 1023) { revertWithCode(0xf001ca11) }
                verifyFileSystemProof(fd)
                validateExistingDescriptor()
                let fileSize := fileSystemProofU64(7)
                let descriptorID := fileSystemProofWord(19)
                let descriptorOffset := fileSystemProofU64(20)
                let update := 0
                switch whence
                case 0 { // SEEK_SET
                    switch gt(offset, fileSize)
                    case 1 { errCode := toU64(0x22) }
                    default {
                        out := offset
                        update := 1
                    }
                }
                case 1 { // SEEK_CUR
                    switch gt(offset, sub64(fileSize, descriptorOffset))
                    case 1 { errCode := toU64(0x22) }
                    default {
                        out := add64(descriptorOffset, offset)
                        update := 1
                    }
                }
                case 2 { // SEEK_END
                    switch gt(offset, fileSize)
                    case 1 { errCode := toU64(0x22) }
                    default {
                        out := sub64(fileSize, offset)
                        update := 1
                    }
                }
                default { revertWithCode(0xf001ca11) }
                if update { setUpdatedFileSystemRoot(fd, fileSystemProofU64(1), descriptorID, out) }
            }

            //
            // CSR (control and status registers) functions
            //
            // Only the floating-point CSRs exist: 0x001 fflags, 0x002 frm, 0x003 fcsr. They
            // are three views of one register, with frm in bits [7:5] of fcsr.
            //
            // Every other CSR reads as zero and ignores writes, mirroring fast/vm.go and
            // slow/vm.go. That is a real limitation rather than a safe default, but it cannot
            // be tightened: guest startup code legitimately touches machine CSRs (the
            // riscv-tests preamble reads mhartid and writes mtvec) and this machine has no
            // privilege levels or trap handling.
            // RISC-V NaN-boxing, mirroring fast/vm.go: a float register holds a valid single
            // only if its upper 32 bits are all ones; anything else reads as the canonical
            // single NaN.
            function unboxF32(v) -> out {
                out := and64(v, u32Mask())
                if iszero(eq(and64(shr64(toU64(32), v), u32Mask()), u32Mask())) {
                    out := shl64(toU64(16), shortToU64(0x7FC0)) // 0x7FC00000
                }
            }

            function getFCSR() -> out {
                out := readState(stateOffsetFCSR(), stateSizeFCSR())
            }
            function setFCSR(v) {
                writeState(stateOffsetFCSR(), stateSizeFCSR(), v)
            }

            function readCSR(num) -> out {
                out := 0
                switch num
                case 0x001 { out := and64(getFCSR(), toU64(31)) }                       // fflags
                case 0x002 { out := and64(shr64(toU64(5), getFCSR()), toU64(7)) }       // frm
                case 0x003 { out := and64(getFCSR(), toU64(255)) }                      // fcsr
            }

            function writeCSR(num, v) {
                switch num
                case 0x001 {
                    setFCSR(or64(and64(getFCSR(), shortToU64(0xE0)), and64(v, toU64(31))))
                } case 0x002 {
                    setFCSR(or64(and64(getFCSR(), toU64(31)), shl64(toU64(5), and64(v, toU64(7)))))
                } case 0x003 {
                    setFCSR(and64(v, toU64(255)))
                }
            }

            function updateCSR(num, v, mode) -> out {
                out := readCSR(num)
                switch mode
                case 1 { // ?01 = CSRRW(I)
                } case 2 { // ?10 = CSRRS(I)
                    v := or64(out, v)
                } case 3 { // ?11 = CSRRC(I)
                    v := and64(out, not64(v))
                } default {
                    revertWithCode(0xbadc0de0) // unkwown CSR mode
                }
                writeCSR(num, v)
            }

            //
            // Preimage oracle interactions
            //
            function writePreimageKey(addr, count) -> out {
                // adjust count down, so we only have to read a single 32 byte leaf of memory
                let alignment := and64(addr, toU64(31))
                let maxData := sub64(toU64(32), alignment)
                if gt64(count, maxData) {
                    count := maxData
                }

                let dat := b32asBEWord(getMemoryB32(sub64(addr, alignment), 1))
                // shift out leading bits
                dat := shl(u64ToU256(shl64(toU64(3), alignment)), dat)
                // shift to right end, remove trailing bits
                dat := shr(u64ToU256(shl64(toU64(3), sub64(toU64(32), count))), dat)

                let bits := shl(toU256(3), u64ToU256(count))

                let preImageKey := getPreimageKey()

                // Append to key content by bit-shifting
                let key := b32asBEWord(preImageKey)
                key := shl(bits, key)
                key := or(key, dat)

                // We reset the pre-image value offset back to 0 (the right part of the merkle pair)
                setPreimageKey(beWordAsB32(key))
                setPreimageOffset(toU64(0))
                out := count
            }

            function readPreimagePart(key, offset) -> dat, datlen {
                let addr := sload(preimageOraclePos()) // calling Oracle.readPreimage(bytes32,uint256)
                let memPtr := mload(0x40) // get pointer to free memory for preimage interactions
                mstore(memPtr, shl(224, 0xe03110e1)) // (32-4)*8=224: right-pad the function selector, and then store it as prefix
                mstore(add(memPtr, 0x04), key)
                mstore(add(memPtr, 0x24), offset)
                let cgas := 100000 // TODO change call gas
                let res := call(cgas, addr, 0, memPtr, 0x44, 0x00, 0x40) // output into scratch space
                if res { // 1 on success
                    dat := mload(0x00)
                    datlen := mload(0x20)
                    leave
                }
                revertWithCode(0xbadf00d0)
            }

            function localize(preImageKey, localContext_) -> localizedKey {
                // TODO: deduplicate definition of localize using lib
                // Grab the current free memory pointer to restore later.
                let ptr := mload(0x40)
                // Store the local data key and caller next to each other in memory for hashing.
                mstore(0, preImageKey)
                mstore(0x20, caller())
                mstore(0x40, localContext_)
                // Localize the key with the above `localize` operation.
                localizedKey := or(and(keccak256(0, 0x60), not(shl(248, 0xFF))), shl(248, 1))
                // Restore the free memory pointer.
                mstore(0x40, ptr)
            }

            function readPreimageValue(addr, count, localContext_) -> out {
                let preImageKey := getPreimageKey()
                let offset := getPreimageOffset()
                // If the preimage key is a local key, localize it in the context of the caller.
                let preImageKeyPrefix := shr(248, preImageKey) // 256-8=248
                if eq(preImageKeyPrefix, 1) {
                    preImageKey := localize(preImageKey, localContext_)
                }
                // make call to pre-image oracle contract
                let pdatB32, pdatlen := readPreimagePart(preImageKey, offset)
                if iszero64(pdatlen) { // EOF
                    out := toU64(0)
                    leave
                }
                let alignment := and64(addr, toU64(31))    // how many bytes addr is offset from being left-aligned
                let maxData := sub64(toU64(32), alignment) // higher alignment leaves less room for data this step
                if gt64(count, maxData) {
                    count := maxData
                }
                if gt64(count, pdatlen) { // cannot read more than pdatlen
                    count := pdatlen
                }

                let bits := shl64(toU64(3), sub64(toU64(32), count))             // 32-count, in bits
                let mask := not(sub(shl(u64ToU256(bits), toU256(1)), toU256(1))) // left-aligned mask for count bytes
                let alignmentBits := u64ToU256(shl64(toU64(3), alignment))
                mask := shr(alignmentBits, mask)                  // mask of count bytes, shifted by alignment
                let pdat := shr(alignmentBits, b32asBEWord(pdatB32)) // pdat, shifted by alignment

                // update pre-image reader with updated offset
                let newOffset := add64(offset, count)
                setPreimageOffset(newOffset)

                let node := getMemoryB32(sub64(addr, alignment), 1)
                let dat := and(b32asBEWord(node), not(mask)) // keep old bytes outside of mask
                dat := or(dat, and(pdat, mask))           // fill with bytes from pdat
                setMemoryB32(sub64(addr, alignment), beWordAsB32(dat), 1)
                out := count
            }

            //
            // Syscall handling
            //
            function sysCall(localContext_) {
                let a7 := getRegister(toU64(17))
                switch a7
                case 93 { // exit the calling thread. No multi-thread support yet, so just exit.
                    let a0 := getRegister(toU64(10))
                    setExitCode(and(a0, 0xff))
                    setExited()
                    // program stops here, no need to change registers.
                } case 94 { // exit-group
                    let a0 := getRegister(toU64(10))
                    setExitCode(and(a0, 0xff))
                    setExited()
                } case 214 { // brk
                    // Go sys_linux_riscv64 runtime will only ever call brk(NULL), i.e. first argument (register a0) set to 0.

                    // brk(0) changes nothing about the memory, and returns the current page break
                    let v := shl64(toU64(30), toU64(1)) // set program break at 1 GiB
                    setRegister(toU64(10), v)
                    setRegister(toU64(11), toU64(0)) // no error
                } case 222 { // mmap
                    // A0 = addr (hint)
                    let addr := getRegister(toU64(10))
                    // A1 = n (length)
                    let length := getRegister(toU64(11))
                    // A2 = prot (memory protection type, can ignore)
                    // A3 = flags (shared with other process and or written back to file, can ignore)  // TODO maybe assert the MAP_ANONYMOUS flag is set
                    // A4 = fd (file descriptor, can ignore because we support anon memory only)
                    // A5 = offset (offset in file, we don't support any non-anon memory, so we can ignore this)

                    // ignore: prot, flags, fd, offset
                    // MLVM reserves a fixed 64 GiB per mapping regardless of what was asked
                    // for. Pages are allocated lazily, so over-reserving costs nothing; but
                    // the heap cursor then advances by a constant, and all three
                    // implementations must advance it identically or their post-states
                    // diverge.
                    length := shl(36, 1)

                    switch addr
                    case 0 {
                        // No hint, allocate it ourselves, by as much as the requested length.
                        // Increase the length to align it with desired page size if necessary.
                        let align := and64(length, shortToU64(4095))
                        if align {
                            length := add64(length, sub64(shortToU64(4096), align))
                        }
                        let prevHeap := getHeap()
                        setRegister(toU64(10), prevHeap)
                        setHeap(add64(prevHeap, length)) // increment heap with length
                    }
                    default {
                        // allow hinted memory address (leave it in A0 as return argument)
                    }
                    setRegister(toU64(11), toU64(0)) // no error
                } case 63 { // read
                    let fd := getRegister(toU64(10))    // A0 = fd
                    let addr := getRegister(toU64(11))  // A1 = *buf addr
                    let count := getRegister(toU64(12)) // A2 = count
                    let n := 0
                    let errCode := 0
                    switch fd
                    case 0 { // stdin
                        n := toU64(0) // never read anything from stdin
                        errCode := toU64(0)
                    } case 3 { // hint-read
                        // say we read it all, to continue execution after reading the hint-write ack response
                        n := count
                        errCode := toU64(0)
                    } case 5 { // preimage read
                        n := readPreimageValue(addr, count, localContext_)
                        errCode := toU64(0)
                    } default {
                        n, errCode := readStagedFile(fd, addr, count)
                    }
                    setRegister(toU64(10), n)
                    setRegister(toU64(11), errCode)
                } case 64 { // write
                    let fd := getRegister(toU64(10))    // A0 = fd
                    let addr := getRegister(toU64(11))  // A1 = *buf addr
                    let count := getRegister(toU64(12)) // A2 = count
                    let n := 0
                    let errCode := 0
                    switch fd
                    case 1 { // stdout
                        n := count // write completes fully in single instruction step
                        errCode := toU64(0)
                    } case 2 { // stderr
                        n := count // write completes fully in single instruction step
                        errCode := toU64(0)
                    } case 4 { // hint-write
                        n := count
                        errCode := toU64(0)
                    } case 6 { // pre-image key-write
                        n := writePreimageKey(addr, count)
                        errCode := toU64(0) // no error
                    } default { // any other file, including (3) hint read (5) preimage read
                        n := u64Mask()         //  -1 (writing error)
                        errCode := toU64(0x4d) // EBADF
                    }
                    setRegister(toU64(10), n)
                    setRegister(toU64(11), errCode)
                } case 25 { // fcntl - file descriptor manipulation / info lookup
                    let fd := getRegister(toU64(10))  // A0 = fd
                    let cmd := getRegister(toU64(11)) // A1 = cmd
                    let out := 0
                    let errCode := 0
                    switch cmd
                    case 0x1 { // F_GETFD: get the close-on-exec flag. See fast/vm.go.
                        switch lt(fd, 7)
                        case 1 { out := toU64(0) }
                        default {
                            switch stagedDescriptorExists(fd)
                            case 1 { out := toU64(0) }
                            default {
                                out := u64Mask()
                                errCode := toU64(0x4d) // EBADF
                            }
                        }
                    } case 0x3 { // F_GETFL: get file descriptor flags
                        switch fd
                        case 0 { // stdin
                            out := toU64(0) // O_RDONLY
                        } case 1 { // stdout
                            out := toU64(1) // O_WRONLY
                        } case 2 { // stderr
                            out := toU64(1) // O_WRONLY
                        } case 3 { // hint-read
                            out := toU64(0) // O_RDONLY
                        } case 4 { // hint-write
                            out := toU64(1) // O_WRONLY
                        } case 5 { // pre-image read
                            out := toU64(0) // O_RDONLY
                        } case 6 { // pre-image write
                            out := toU64(1) // O_WRONLY
                        } default {
                            switch stagedDescriptorExists(fd)
                            case 1 { out := toU64(0) } // staged files are O_RDONLY
                            default {
                                out := u64Mask()
                                errCode := toU64(0x4d) // EBADF
                            }
                        }
                    } default { // no other commands: don't allow changing flags, duplicating FDs, etc.
                        out := u64Mask()
                        errCode := toU64(0x16) // EINVAL (cmd not recognized by this kernel)
                    }
                    setRegister(toU64(10), out)
                    setRegister(toU64(11), errCode) // EBADF
                } case 278 { // getrandom - no-op, for now
                    let count := getRegister(toU64(11))
                    setRegister(toU64(10), count)
                    setRegister(toU64(11), toU64(0))
                } case 56 { // openat
                    let pathname := getRegister(toU64(11))
                    let out, errCode := openStagedFile(pathname)
                    setRegister(toU64(10), out)
                    setRegister(toU64(11), errCode)
                } case 57 { // close
                    let fd := getRegister(toU64(10))
                    closeStagedFile(fd)
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 62 { // lseek
                    let fd := getRegister(toU64(10))
                    let offset := getRegister(toU64(11))
                    let whence := getRegister(toU64(12))
                    let out, errCode := seekStagedFile(fd, offset, whence)
                    setRegister(toU64(10), out)
                    setRegister(toU64(11), errCode)
                } case 258 { // riscv_hwprobe - refused, so the trace cannot depend on host CPU features
                    setRegister(toU64(10), u64Mask())
                    setRegister(toU64(11), toU64(38)) // ENOSYS
                } case 123 { // sched_getaffinity - hardcode to indicate affinity with any cpu-set mask
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 226 { // mprotect - ignore any memory protection requests
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 124 { // sched_yield - nothing to yield, synchronous execution only, for now
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 73 {// ppoll_time32 - ignore any poll request
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 113 { // clock_gettime
                    let addr := getRegister(toU64(11)) // addr of timespec struct
                    // write 1337s + 42ns as time
                    storeMemUnaligned(addr, toU64(8), shortToU256(1337), 1, 0xff)
                    storeMemUnaligned(add64(addr, toU64(8)), toU64(8), toU256(42), 2, 0xff)
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 135 { // rt_sigprocmask - ignore any sigset changes
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 132 { // sigaltstack - ignore any hints of an alternative signal receiving stack addr
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 178 { // gettid - hardcode to 0
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 179 { // sysinfo - fixed committed view matching fast/vm.go
                    let addr := getRegister(toU64(10))
                    // RV64 Linux uapi layout: uptime@0, totalram@32, freeram@40,
                    // mem_unit@104. Seven 16-byte writes cover the padded 112-byte struct.
                    let proofIndex := 1
                    proofIndex := storeSysinfoChunk(addr, toU64(0), shortToU256(1337), proofIndex)
                    proofIndex := storeSysinfoChunk(addr, toU64(16), toU256(0), proofIndex)
                    proofIndex := storeSysinfoChunk(
                        addr,
                        toU64(32),
                        or(shl(30, 1), shl(94, 1)),
                        proofIndex
                    )
                    proofIndex := storeSysinfoChunk(addr, toU64(48), toU256(0), proofIndex)
                    proofIndex := storeSysinfoChunk(addr, toU64(64), toU256(0), proofIndex)
                    proofIndex := storeSysinfoChunk(addr, toU64(80), toU256(0), proofIndex)
                    proofIndex := storeSysinfoChunk(addr, toU64(96), shl(64, 1), proofIndex)
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 134 { // rt_sigaction - no-op, we never send signals, and thus need no sig handler info
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 220 { // clone - not supported
                    setRegister(toU64(10), toU64(1))
                    setRegister(toU64(11), toU64(0))
                } case 163 { // getrlimit
                    let res := getRegister(toU64(10))
                    let addr := getRegister(toU64(11))
                    switch res
                    case 0x7 {  // RLIMIT_NOFILE
                        // first 8 bytes: soft limit. 1024 file handles max open
                        // second 8 bytes: hard limit
                        storeMemUnaligned(addr, toU64(16), or(shortToU256(1024), shl(toU256(64), shortToU256(1024))), 1, 2)
                    } default {
                        revertWithCode(0xf0012) // unrecognized resource limit lookup
                    }
                } case 233 { // madvise - ignored
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 96 { // set_tid_address - ignored
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 99 { // get_robust_list - ignored
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 261 { // prlimit64 -- unsupported, we have getrlimit, is prlimit64 even called?
                    revertWithCode(0xf001ca11) // unsupported system call
                } case 78 { // readlinkat - ignored
                    setRegister(toU64(10), toU64(0))
                    setRegister(toU64(11), toU64(0))
                } case 422 { // futex - not supported, for now
                    revertWithCode(0xf001ca11) // unsupported system call
                } case 101 { // nanosleep - not supported, for now
                    revertWithCode(0xf001ca11) // unsupported system call
                } default {
                    revertWithCode(0xf001ca11) // unrecognized system call
                }
            }

            //
            // Instruction execution
            //

            if getExited() { // early exit if we can
                mstore(0, computeStateHash())
                return(0, 0x20)
            }
            setStep(add64(getStep(), toU64(1)))

            let _pc := getPC()
            let instr := loadMem(_pc, toU64(4), false, 0, 0xff) // raw instruction

            // these fields are ignored if not applicable to the instruction type / opcode
            let opcode := parseOpcode(instr)
            let rd := parseRd(instr) // destination register index
            let funct3 := parseFunct3(instr)
            let rs1 := parseRs1(instr) // source register 1 index
            let rs2 := parseRs2(instr) // source register 2 index
            let funct7 := parseFunct7(instr)

            switch opcode
            case 0x03 { // 000_0011: memory loading
                // LB, LH, LW, LD, LBU, LHU, LWU
                let imm := parseImmTypeI(instr)
                let signed := iszero64(and64(funct3, toU64(4)))      // 4 = 100 -> bitflag
                let size := shl64(and64(funct3, toU64(3)), toU64(1)) // 3 = 11 -> 1, 2, 4, 8 bytes size
                let rs1Value := getRegister(rs1)
                let memIndex := add64(rs1Value, signExtend64(imm, toU64(11)))
                let rdValue := loadMem(memIndex, size, signed, 1, 2)
                setRegister(rd, rdValue)
                setPC(add64(_pc, toU64(4)))
            } case 0x23 { // 010_0011: memory storing
                // SB, SH, SW, SD
                let imm := parseImmTypeS(instr)
                let size := shl64(funct3, toU64(1))
                let value := getRegister(rs2)
                let rs1Value := getRegister(rs1)
                let memIndex := add64(rs1Value, signExtend64(imm, toU64(11)))
                storeMem(memIndex, size, value, 1, 2)
                setPC(add64(_pc, toU64(4)))
            } case 0x63 { // 110_0011: branching
                let rs1Value := getRegister(rs1)
                let rs2Value := getRegister(rs2)
                let branchHit := toU64(0)
                switch funct3
                case 0 { // 000 = BEQ
                    branchHit := eq64(rs1Value, rs2Value)
                } case 1 { // 001 = BNE
                    branchHit := and64(not64(eq64(rs1Value, rs2Value)), toU64(1))
                } case 4 { // 100 = BLT
                    branchHit := slt64(rs1Value, rs2Value)
                } case 5 { // 101 = BGE
                    branchHit := and64(not64(slt64(rs1Value, rs2Value)), toU64(1))
                } case 6 { // 110 = BLTU
                    branchHit := lt64(rs1Value, rs2Value)
                } case 7 { // 111 := BGEU
                    branchHit := and64(not64(lt64(rs1Value, rs2Value)), toU64(1))
                }
                switch branchHit
                case 0 {
                    _pc := add64(_pc, toU64(4))
                } default {
                    let imm := parseImmTypeB(instr)
                    // imm12 is a signed offset, in multiples of 2 bytes.
			        // So it's really 13 bits with a hardcoded 0 bit.
                    _pc := add64(_pc, imm)
                }
                // not like the other opcodes: nothing to write to rd register, and PC has already changed
                setPC(_pc)
            } case 0x13 { // 001_0011: immediate arithmetic and logic
		        let rs1Value := getRegister(rs1)
                let imm := parseImmTypeI(instr)
                let rdValue := 0
                switch funct3
                case 0 { // 000 = ADDI
                    rdValue := add64(rs1Value, imm)
                } case 1 { // 001 = SLLI
                    rdValue := shl64(and64(imm, toU64(0x3F)), rs1Value) // lower 6 bits in 64 bit mode
                } case 2 { // 010 = SLTI
                    rdValue := slt64(rs1Value, imm)
                } case 3 { // 011 = SLTIU
                    rdValue := lt64(rs1Value, imm)
                } case 4 { // 100 = XORI
                    rdValue := xor64(rs1Value, imm)
                } case 5 { // 101 = SR~
                    switch shr64(toU64(6), imm) // in rv64i the top 6 bits select the shift type
                    case 0x00 { // 000000 = SRLI
                        rdValue := shr64(and64(imm, toU64(0x3F)), rs1Value) // lower 6 bits in 64 bit mode
                    } case 0x10 { // 010000 = SRAI
                        rdValue := sar64(and64(imm, toU64(0x3F)), rs1Value) // lower 6 bits in 64 bit mode
                    }
                } case 6 { // 110 = ORI
                    rdValue := or64(rs1Value, imm)
                } case 7 { // 111 = ANDI
                    rdValue := and64(rs1Value, imm)
                }
                setRegister(rd, rdValue)
                setPC(add64(_pc, toU64(4)))
            } case 0x1B { // 001_1011: immediate arithmetic and logic signed 32 bit
		        let rs1Value := getRegister(rs1)
                let imm := parseImmTypeI(instr)
                let rdValue := 0
                switch funct3
                case 0 { // 000 = ADDIW
                    rdValue := mask32Signed64(add64(rs1Value, imm))
                } case 1 { // 001 = SLLIW
                    rdValue := mask32Signed64(shl64(and64(imm, toU64(0x1F)), rs1Value))
                } case 5 { // 101 = SR~
                    let shamt := and64(imm, toU64(0x1F))
                    switch shr64(toU64(6), imm) // in rv64i the top 6 bits select the shift type
                    case 0x00 { // 000000 = SRLIW
                        rdValue := signExtend64(shr64(shamt, and64(rs1Value, u32Mask())), toU64(31))
                    } case 0x10 { // 010000 = SRAIW
                        rdValue := signExtend64(shr64(shamt, and64(rs1Value, u32Mask())), sub64(toU64(31), shamt))
                    }
                }
                setRegister(rd, rdValue)
                setPC(add64(_pc, toU64(4)))
            } case 0x33 { // 011_0011: register arithmetic and logic
		        let rs1Value := getRegister(rs1)
		        let rs2Value := getRegister(rs2)
                let rdValue := 0
                switch funct7
                case 1 { // RV M extension
                    switch funct3
                    case 0 { // 000 = MUL: signed x signed
                        rdValue := mul64(rs1Value, rs2Value)
                    } case 1 { // 001 = MULH: upper bits of signed x signed
                        rdValue := u256ToU64(shr(toU256(64), mul(signExtend64To256(rs1Value), signExtend64To256(rs2Value))))
                    } case 2 { // 010 = MULHSU: upper bits of signed x unsigned
                        rdValue := u256ToU64(shr(toU256(64), mul(signExtend64To256(rs1Value), u64ToU256(rs2Value))))
                    } case 3 { // 011 = MULHU: upper bits of unsigned x unsigned
                        rdValue := u256ToU64(shr(toU256(64), mul(u64ToU256(rs1Value), u64ToU256(rs2Value))))
                    } case 4 { // 100 = DIV
                        switch rs2Value
                        case 0 {
                            rdValue := u64Mask()
                        } default {
                            rdValue := sdiv64(rs1Value, rs2Value)
                        }
                    } case 5 { // 101 = DIVU
                        switch rs2Value
                        case 0 {
                            rdValue := u64Mask()
                        } default {
                            rdValue := div64(rs1Value, rs2Value)
                        }
                    } case 6 { // 110 = REM
                        switch rs2Value
                        case 0 {
                            rdValue := rs1Value
                        } default {
                            rdValue := smod64(rs1Value, rs2Value)
                        }
                    } case 7 { // 111 = REMU
                        switch rs2Value
                        case 0 {
                            rdValue := rs1Value
                        } default {
                            rdValue := mod64(rs1Value, rs2Value)
                        }
                    }
                } default {
                    switch funct3
                    case 0 { // 000 = ADD/SUB
                        switch funct7
                        case 0x00 { // 0000000 = ADD
                            rdValue := add64(rs1Value, rs2Value)
                        } case 0x20 { // 0100000 = SUB
                            rdValue := sub64(rs1Value, rs2Value)
                        }
                    } case 1 { // 001 = SLL
                        rdValue := shl64(and64(rs2Value, toU64(0x3F)), rs1Value) // only the low 6 bits are consider in RV6VI
                    } case 2 { // 010 = SLT
                        rdValue := slt64(rs1Value, rs2Value)
                    } case 3 { // 011 = SLTU
                        rdValue := lt64(rs1Value, rs2Value)
                    } case 4 { // 100 = XOR
                        rdValue := xor64(rs1Value, rs2Value)
                    } case 5 { // 101 = SR~
                        switch funct7
                        case 0x00 { // 0000000 = SRL
                            rdValue := shr64(and64(rs2Value, toU64(0x3F)), rs1Value) // logical: fill with zeroes
                        } case 0x20 { // 0100000 = SRA
                            rdValue := sar64(and64(rs2Value, toU64(0x3F)), rs1Value) // arithmetic: sign bit is extended
                        }
                    } case 6 { // 110 = OR
                        rdValue := or64(rs1Value, rs2Value)
                    } case 7 { // 111 = AND
                        rdValue := and64(rs1Value, rs2Value)
                    }
                }
                setRegister(rd, rdValue)
                setPC(add64(_pc, toU64(4)))
            } case 0x3B { // 011_1011: register arithmetic and logic in 32 bits
                let rs1Value := getRegister(rs1)
                let rs2Value := getRegister(rs2)
                let rdValue := 0
                switch funct7
                case 1 { // RV M extension
                    switch funct3
                    case 0 { // 000 = MULW
                        rdValue := mask32Signed64(mul64(and64(rs1Value, u32Mask()), and64(rs2Value, u32Mask())))
                    } case 4 { // 100 = DIVW
                        switch rs2Value
                        case 0 {
                            rdValue := u64Mask()
                        } default {
                            rdValue := mask32Signed64(sdiv64(mask32Signed64(rs1Value), mask32Signed64(rs2Value)))
                        }
                    } case 5 { // 101 = DIVUW
                        switch rs2Value
                        case 0 {
                            rdValue := u64Mask()
                        } default {
                            rdValue := mask32Signed64(div64(and64(rs1Value, u32Mask()), and64(rs2Value, u32Mask())))
                        }
                    } case 6 { // 110 = REMW
                        switch rs2Value
                        case 0 {
                            rdValue := mask32Signed64(rs1Value)
                        } default {
                            rdValue := mask32Signed64(smod64(mask32Signed64(rs1Value), mask32Signed64(rs2Value)))
                        }
                    } case 7 { // 111 = REMUW
                        switch rs2Value
                        case 0 {
                            rdValue := mask32Signed64(rs1Value)
                        } default {
                            rdValue := mask32Signed64(mod64(and64(rs1Value, u32Mask()), and64(rs2Value, u32Mask())))
                        }
                    }
                } default {
                    switch funct3
                    case 0 { // 000 = ADDW/SUBW
                        switch funct7
                        case 0x00 { // 0000000 = ADDW
                            rdValue := mask32Signed64(add64(and64(rs1Value, u32Mask()), and64(rs2Value, u32Mask())))
                        } case 0x20 { // 0100000 = SUBW
                            rdValue := mask32Signed64(sub64(and64(rs1Value, u32Mask()), and64(rs2Value, u32Mask())))
                        }
                    } case 1 { // 001 = SLLW
                        rdValue := mask32Signed64(shl64(and64(rs2Value, toU64(0x1F)), rs1Value))
                    } case 5 { // 101 = SR~
                        let shamt := and64(rs2Value, toU64(0x1F))
                        switch funct7
                        case 0x00 { // 0000000 = SRLW
                            rdValue := signExtend64(shr64(shamt, and64(rs1Value, u32Mask())), toU64(31))
                        } case 0x20 { // 0100000 = SRAW
                            rdValue := signExtend64(shr64(shamt, and64(rs1Value, u32Mask())), sub64(toU64(31), shamt))
                        }
                    }
                }
                setRegister(rd, rdValue)
                setPC(add64(_pc, toU64(4)))
            } case 0x37 { // 011_0111: LUI = Load upper immediate
                let imm := parseImmTypeU(instr)
                let rdValue := shl64(toU64(12), imm)
                setRegister(rd, rdValue)
                setPC(add64(_pc, toU64(4)))
            } case 0x17 { // 001_0111: AUIPC = Add upper immediate to PC
                let imm := parseImmTypeU(instr)
                let rdValue := add64(_pc, signExtend64(shl64(toU64(12), imm), toU64(31)))
                setRegister(rd, rdValue)
                setPC(add64(_pc, toU64(4)))
            } case 0x6F { // 110_1111: JAL = Jump and link
                let imm := parseImmTypeJ(instr)
                let rdValue := add64(_pc, toU64(4))
                setRegister(rd, rdValue)
                setPC(add64(_pc, signExtend64(shl64(toU64(1), imm), toU64(20)))) // signed offset in multiples of 2 bytes (last bit is there, but ignored)
            } case 0x67 { // 110_0111: JALR = Jump and link register
		        let rs1Value := getRegister(rs1)
                let imm := parseImmTypeI(instr)
                let rdValue := add64(_pc, toU64(4))
                setRegister(rd, rdValue)
                setPC(and64(add64(rs1Value, signExtend64(imm, toU64(11))), xor64(u64Mask(), toU64(1)))) // least significant bit is set to 0
            } case 0x73 { // 111_0011: environment things
                switch funct3
                case 0 { // 000 = ECALL/EBREAK
                    switch shr64(toU64(20), instr) // I-type, top 12 bits
                    case 0 { // imm12 = 000000000000 ECALL
                        sysCall(localContext)
                        setPC(add64(_pc, toU64(4)))
                    } default { // imm12 = 000000000001 EBREAK
                        setPC(add64(_pc, toU64(4))) // ignore breakpoint
                    }
                } default { // CSR instructions
                    let imm := parseCSSR(instr)
                    let value := rs1
                    if iszero64(and64(funct3, toU64(4))) {
                        value := getRegister(rs1)
                    }
                    let mode := and64(funct3, toU64(3))
                    let rdValue := updateCSR(imm, value, mode)
                    setRegister(rd, rdValue)
                    setPC(add64(_pc, toU64(4)))
                }
            } case 0x2F { // 010_1111: RV32A and RV32A atomic operations extension
                // acquire and release bits:
                //   aq := and64(shr64(toU64(1), funct7), toU64(1))
                //   rl := and64(funct7, toU64(1))
                // if none set: unordered
                // if aq is set: no following mem ops observed before acquire mem op
                // if rl is set: release mem op not observed before earlier mem ops
                // if both set: sequentially consistent
                // These are no-op here because there is no pipeline of mem ops to acquire/release.

                // 0b010 == RV32A W variants
                // 0b011 == RV64A D variants
                let size := shl64(funct3, toU64(1))
                if lt64(size, toU64(4)) {
                    revertWithCode(0xbada70) // bad AMO size
                }
                let addr := getRegister(rs1)
                // TODO check if addr is aligned

                let op := shr64(toU64(2), funct7)
                switch op
                case 0x2 { // 00010 = LR = Load Reserved
                    let v := loadMem(addr, size, true, 1, 2)
                    setRegister(rd, v)
                    setLoadReservation(addr)
                } case 0x3 { // 00011 = SC = Store Conditional
                    let rdValue := toU64(1)
                    if eq64(addr, getLoadReservation()) {
                        let rs2Value := getRegister(rs2)
                        storeMem(addr, size, rs2Value, 1, 2)
                        rdValue := toU64(0)
                    }
                    setRegister(rd, rdValue)
                    setLoadReservation(toU64(0))
                } default { // AMO: Atomic Memory Operation
                    let rs2Value := getRegister(rs2)
                    if eq64(size, toU64(4)) {
                        rs2Value := mask32Signed64(rs2Value)
                    }
                    let value := rs2Value
                    let v := loadMem(addr, size, true, 1, 2)
                    let rdValue := v
                    switch op
                    case 0x0 { // 00000 = AMOADD = add
                        v := add64(v, value)
                    } case 0x1 { // 00001 = AMOSWAP
                        v := value
                    } case 0x4 { // 00100 = AMOXOR = xor
                        v := xor64(v, value)
                    } case 0x8 { // 01000 = AMOOR = or
                        v := or64(v, value)
                    } case 0xc { // 01100 = AMOAND = and
                        v := and64(v, value)
                    } case 0x10 { // 10000 = AMOMIN = min signed
                        if slt64(value, v) {
                            v := value
                        }
                    } case 0x14 { // 10100 = AMOMAX = max signed
                        if sgt64(value, v) {
                            v := value
                        }
                    } case 0x18 { // 11000 = AMOMINU = min unsigned
                        if lt64(value, v) {
                            v := value
                        }
                    } case 0x1c { // 11100 = AMOMAXU = max unsigned
                        if gt64(value, v) {
                            v := value
                        }
                    } default {
                        revertWithCode(0xf001a70) // unknown atomic operation
                    }
                    storeMem(addr, size, v, 1, 3) // after overwriting 1, proof 2 is no longer valid
                    setRegister(rd, rdValue)
                }
                setPC(add64(_pc, toU64(4)))
            } case 0x0F { // 000_1111: fence
                // Used to impose additional ordering constraints; flushing the mem operation pipeline.
                // This VM doesn't have a pipeline, nor additional harts, so this is a no-op.
                // FENCE / FENCE.TSO / FENCE.I all no-op: there's nothing to synchronize.
                setPC(add64(_pc, toU64(4)))
            } case 0x07 { // FLW/FLD - loads move bits, no arithmetic
                let width := parseWidth(instr)
                let imm := parseImmTypeI(instr)
                let rs1Value := getRegister(rs1)
                let memIndex := add64(rs1Value, signExtend64(imm, toU64(11)))
                let rdValue := 0
                switch width
                case 2 { // FLW, NaN-boxed into the upper half
                    rdValue := loadMem(memIndex, toU64(4), false, 1, 2)
                    rdValue := or64(rdValue, shl64(toU64(32), u32Mask()))
                } case 3 { // FLD
                    rdValue := loadMem(memIndex, toU64(8), false, 1, 2)
                }
                setFRegister(rd, rdValue)
                setPC(add64(_pc, toU64(4)))
            } case 0x27 { // FSW/FSD
                let width := parseWidth(instr)
                let imm := parseImmTypeS(instr)
                let value := getFRegister(rs2)
                let rs1Value := getRegister(rs1)
                let memIndex := add64(rs1Value, signExtend64(imm, toU64(11)))
                switch width
                case 2 { storeMem(memIndex, toU64(4), value, 1, 2) }
                case 3 { storeMem(memIndex, toU64(8), value, 1, 2) }
                setPC(add64(_pc, toU64(4)))
            } case 0x53 { // floating point ALU
                //
                // Only the subset that is pure bit manipulation: register moves and sign
                // injection. Everything needing an actual IEEE-754 computation - add, sub,
                // mul, div, sqrt, compare, convert - reverts.
                //
                // Upstream treated all of these as no-ops, which was harmless when nothing
                // executed them for real. Here it would be unsound: the off-chain VM
                // executes F/D through SoftFloat, so a no-op yields a different post-state
                // that still looks well-formed, and the honest party loses the dispute.
                // Reverting turns a soundness bug into a liveness gap.
                //
                let format := parsefmt(instr)
                let width := parseWidth(instr)
                let f5 := parseFunct5(instr)
                switch f5
                case 0x04 { // FSGNJ / FSGNJN / FSGNJX
                    let signMask := shl64(toU64(63), toU64(1))
                    if iszero(format) { signMask := shl64(toU64(31), toU64(1)) }
                    let a := getFRegister(rs1)
                    let b := getFRegister(rs2)
                    if iszero(format) {
                        // NaN-boxing: a 64-bit float register holds a valid single only if
                        // its upper 32 bits are all ones. Anything else reads as the
                        // canonical single NaN. Masking without this check would compute on
                        // the low half of a double.
                        a := unboxF32(a)
                        b := unboxF32(b)
                    }
                    let signBit := 0
                    switch width
                    case 0x0 { signBit := and64(b, signMask) }
                    case 0x1 { signBit := and64(not64(b), signMask) }
                    case 0x2 { signBit := and64(xor64(a, b), signMask) }
                    default { revertWithCode(0xf001f10a) }
                    let rdValue := or64(signBit, and64(a, not64(signMask)))
                    if iszero(format) { rdValue := or64(rdValue, shl64(toU64(32), u32Mask())) }
                    setFRegister(rd, rdValue)
                } case 0x1c { // FMV.X.W / FMV.X.D
                    if iszero(iszero(width)) { revertWithCode(0xf001f10a) } // FCLASS
                    let v := getFRegister(rs1)
                    if iszero(format) { v := signExtend64(and64(v, u32Mask()), toU64(31)) }
                    setRegister(rd, v)
                } case 0x1e { // FMV.W.X / FMV.D.X
                    let v := getRegister(rs1)
                    // The result is a single, so it is NaN-boxed on the way in.
                    if iszero(format) { v := or64(and64(v, u32Mask()), shl64(toU64(32), u32Mask())) }
                    setFRegister(rd, v)
                } default { revertWithCode(0xf001f10a) } // needs SoftFloat
                setPC(add64(_pc, toU64(4)))
            } case 0x43 { revertWithCode(0xf001f10a) // FMADD - needs SoftFloat
            } case 0x47 { revertWithCode(0xf001f10a) // FMSUB
            } case 0x4b { revertWithCode(0xf001f10a) // FNMSUB
            } case 0x4f { revertWithCode(0xf001f10a) // FNMADD
            } default {
                revertWithCode(0xf001c0de) // unknown instruction opcode
            }

            mstore(0, computeStateHash())
            return(0, 0x20)
        }
    }
}
