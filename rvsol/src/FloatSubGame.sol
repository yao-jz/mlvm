// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;

/// @title FloatSubGame
/// @notice An experimental on-chain boundary for a nested f64 terminal component.
///
///         `RISCV.sol` executes RV64IMA. It cannot execute `FADD.D`, and giving it the
///         ability would mean writing IEEE-754 in Yul: thousands of lines of
///         consensus-critical bit manipulation, plus an audit.
///
///         This is the alternative. The same operation compiled for a target *without*
///         floating-point instructions becomes an operation-dependent trace of integer
///         instructions from `compiler_builtins`' soft-float. Those integer steps can be
///         checked by `RISCV.sol`.
///
/// @dev    **This is not a complete dispute game.** The functions derive a nested pre-state
///         from caller-supplied operands and read a result from a caller-supplied post-state
///         and proof. Nothing here binds the operands to an outer VM witness, establishes
///         the nested post-state through an interactive game, applies the result and flags
///         to the outer state, or implements trace commitments, bonds, and timeouts.
///
///         The contract adds no instruction semantics: the nested machine is itself a
///         RISC-V machine, and tests exercise its integer steps through `RISCV.sol.step`.
///
///         Written in plain Solidity rather than Yul on purpose. `RISCV.sol` is Yul because
///         step gas dominates there; this boundary favors readability while the protocol
///         integration remains experimental.
contract FloatSubGame {
    /// @notice Fixed I/O addresses inside the nested machine.
    ///
    ///         The nested pre-state must be a pure function of (image, op, a, b): no
    ///         arguments, no environment, no stack-passed data, no clock. Both parties
    ///         derive byte-identical states or the sub-game is meaningless. That is why the
    ///         co-processor reads its operands from fixed absolute addresses.
    ///
    ///         The layout is chosen so the four inputs occupy bytes [0, 32) — exactly one
    ///         merkle leaf — and the result occupies the next 32 bytes. Because `IO_BASE` is
    ///         64-byte aligned those two leaves are *siblings*, so a single 60-node branch
    ///         carries both: the inputs are written into `proof[0]` and the result is read
    ///         out of `proof[1]`.
    ///
    ///             +0   op      +8   a      +16  b      +24  c        <- leaf, proof[0]
    ///             +32  result  +40  fflags                          <- sibling, proof[1]
    uint64 public constant IO_BASE = 0x3000000000000000;

    /// @notice Operation selectors. Must match the `OP_*` block in the co-processor and the
    ///         `FloatOp*` constants in `fast/floatsubgame.go` exactly.
    ///
    ///         The two-operand arithmetic keeps the RISC-V `funct5` values so the outer VM's
    ///         decode passes straight through; the rest are MLVM's own, because RISC-V
    ///         distinguishes them by opcode and by the `rs2` field rather than by `funct5`.
    ///         FMIN and FMAX share funct5 0x05 and are told apart by funct3, so FMAX is 0x15.
    uint64 public constant OP_ADD = 0x00;
    uint64 public constant OP_SUB = 0x01;
    uint64 public constant OP_MUL = 0x02;
    uint64 public constant OP_DIV = 0x03;
    uint64 public constant OP_MIN = 0x05;
    uint64 public constant OP_SQRT = 0x0B;
    uint64 public constant OP_MAX = 0x15;

    /// @notice The three-operand multiply-add family, RISC-V opcodes 0x43/0x47/0x4B/0x4F.
    uint64 public constant OP_MADD = 0x20;
    uint64 public constant OP_NMADD = 0x23;

    /// @notice Integer to double (0x30–0x33) and double to integer (0x38–0x3B).
    uint64 public constant OP_CVT_D_W = 0x30;
    uint64 public constant OP_CVT_D_LU = 0x33;
    uint64 public constant OP_CVT_W_D = 0x38;
    uint64 public constant OP_CVT_LU_D = 0x3B;

    /// @notice Comparisons, which write an integer register rather than a float one.
    uint64 public constant OP_EQ = 0x40;
    uint64 public constant OP_LE = 0x42;

    /// @notice Byte length of an MLVM state witness.
    uint256 internal constant STATE_SIZE = 674;

    /// @notice Merkle branch length: a 64-bit address down to a 32-byte leaf, leaf included.
    uint256 internal constant PROOF_LEN = 60;

    /// @notice Witness field offsets, mirroring `fast.VMState.EncodeWitness`.
    uint256 internal constant OFF_MEM_ROOT = 0;
    uint256 internal constant OFF_PC = 72;
    uint256 internal constant OFF_EXIT_CODE = 80;
    uint256 internal constant OFF_EXITED = 81;
    uint256 internal constant OFF_REGISTERS = 106;
    uint256 internal constant OFF_FILE_SYSTEM_ROOT = 642;

    /// @notice Root of a depth-10 tree whose 1,024 leaves are all zero.
    bytes32 internal constant EMPTY_FILE_TREE_ROOT =
        0xf9dc3e7fe016e050eff260334f18a5d4fe391d82092319f5964f2e2eb7c1c3a5;

    /// @notice The nested machine's initial stack pointer. The co-processor uses no stack,
    ///         but the compiler's prologue still needs `sp` valid. A fixed value keeps the
    ///         derivation canonical.
    uint64 internal constant INITIAL_SP = 0x1000000000000000;

    /// @notice VM status byte written into the top byte of a state hash.
    uint8 internal constant VM_STATUS_VALID = 0;
    uint8 internal constant VM_STATUS_UNFINISHED = 3;

    /// @notice Merkle root of the committed co-processor program image, before any operands
    ///         are written.
    ///
    /// @dev    This is a protocol constant, and it carries an obligation that cannot be
    ///         checked on chain: **the committed image must contain no floating-point
    ///         instructions.** A build that did would still run correctly off chain and
    ///         nothing here would notice — but its arithmetic would be one `FADD.D` again
    ///         and the nesting would have bought nothing. `fast.assertNoFloatInstructions`
    ///         enforces this at load time by scanning every executable segment; deployment
    ///         must use an image that passes it.
    bytes32 public immutable imageRoot;

    /// @notice Entry point of the co-processor image.
    uint64 public immutable entry;

    error BadProofLength();
    error BadStateLength();
    error ImageRootMismatch();
    error MemProofMismatch();
    error SubGameNotExited();
    error SubGameFaulted(uint8 exitCode);
    error UnknownOperation(uint64 op);

    constructor(bytes32 _imageRoot, uint64 _entry) {
        imageRoot = _imageRoot;
        entry = _entry;
    }

    /// @notice Derive the nested machine's initial state hash from a float operation.
    ///
    ///         A future outer game could call this after isolating an F/D instruction. The
    ///         function converts supplied operation data into a specific nested RISC-V
    ///         pre-state; the caller remains responsible for binding that data to the outer
    ///         witness and for establishing the resulting trace.
    ///
    /// @param op    Operation selector; one of the `OP_*` constants.
    /// @param a     First operand, raw bits. IEEE-754 for the arithmetic, an integer for the
    ///              integer-to-double conversions.
    /// @param b     Second operand. Zero for the single-operand forms.
    /// @param c     Third operand, used only by the multiply-add family. Zero otherwise.
    /// @param proof Merkle branch for `IO_BASE` against `imageRoot`: 60 nodes of 32 bytes,
    ///              element 0 being the leaf itself and element 1 its sibling, which holds
    ///              the result slot.
    /// @return The nested pre-state hash, which is what the sub-game bisects from.
    function nestedPreState(uint64 op, uint64 a, uint64 b, uint64 c, bytes calldata proof)
        external
        view
        returns (bytes32)
    {
        if (!_isKnownOperation(op)) revert UnknownOperation(op);
        if (proof.length != PROOF_LEN * 32) revert BadProofLength();

        // The four inputs occupy bytes [0, 32) of the page at IO_BASE, stored little-endian.
        bytes32 newLeaf = bytes32(
            (uint256(_le64(op)) << 192) | (uint256(_le64(a)) << 128) | (uint256(_le64(b)) << 64)
                | uint256(_le64(c))
        );

        // Two roots up the same branch in one pass: the old one to bind the proof to the
        // committed image, the new one to derive the pre-state. Walking twice would double
        // the keccaks for no additional constraint.
        (bytes32 oldRoot, bytes32 newRoot) = _reconstructBoth(bytes32(proof[0:32]), newLeaf, proof);

        // Without the binding check a disputant could supply a branch from some other memory
        // and derive a pre-state for a program that is not the committed co-processor.
        if (oldRoot != imageRoot) revert ImageRootMismatch();

        return _preStateHash(newRoot);
    }

    /// @notice Read the operation's result out of a completed nested machine.
    ///
    ///         This boundary validates a supplied nested post-state witness and reads its
    ///         result. It requires a clean exit but does not establish how that post-state
    ///         was reached; a future outer protocol must provide that guarantee.
    ///
    ///         Both halves of the instruction's effect come back here. A float instruction
    ///         updates two pieces of committed state — the destination register and `fcsr` —
    ///         so returning only the value would omit part of the proposed transition.
    ///         The flags cost nothing extra to carry: they share the result's merkle leaf.
    ///
    /// @param stateData The nested post-state witness.
    /// @param proof     Merkle branch for `IO_BASE` against that state's memory root.
    /// @return result   The operation's result, raw IEEE-754 bits.
    /// @return flags    The IEEE-754 exceptions raised, in RISC-V fflags order
    ///                  (NX=1, UF=2, OF=4, DZ=8, NV=16), to be OR-ed into `fcsr`.
    function nestedResult(bytes calldata stateData, bytes calldata proof)
        external
        pure
        returns (uint64 result, uint64 flags)
    {
        if (stateData.length != STATE_SIZE) revert BadStateLength();
        if (proof.length != PROOF_LEN * 32) revert BadProofLength();

        if (uint8(stateData[OFF_EXITED]) != 1) revert SubGameNotExited();
        uint8 exitCode = uint8(stateData[OFF_EXIT_CODE]);
        if (exitCode != 0) revert SubGameFaulted(exitCode);

        bytes32 memRoot = bytes32(stateData[OFF_MEM_ROOT:OFF_MEM_ROOT + 32]);
        if (_reconstructRoot(bytes32(proof[0:32]), proof) != memRoot) revert MemProofMismatch();

        // The result is at IO_BASE + 32 and the flags at IO_BASE + 40 — the first and second
        // words of the *sibling* leaf, which the branch above already authenticated as
        // `proof[1]`. Little-endian, so the result occupies the most significant bytes.
        uint256 leaf = uint256(bytes32(proof[32:64]));
        return (_le64(uint64(leaf >> 192)), _le64(uint64(leaf >> 128)));
    }

    /// @notice Whether a selector names an operation the co-processor implements.
    ///
    /// @dev    An unknown selector must be rejected here rather than reaching the nested
    ///         machine. The co-processor also refuses it — exiting non-zero rather than
    ///         quietly computing an addition — but a dispute should fail at the boundary,
    ///         not 130 instructions into a sub-game.
    function _isKnownOperation(uint64 op) internal pure returns (bool) {
        if (op <= OP_DIV) return true; // FADD FSUB FMUL FDIV
        if (op == OP_MIN || op == OP_MAX || op == OP_SQRT) return true;
        if (op >= OP_MADD && op <= OP_NMADD) return true; // the multiply-add family
        if (op >= OP_CVT_D_W && op <= OP_CVT_D_LU) return true; // integer -> double
        if (op >= OP_CVT_W_D && op <= OP_CVT_LU_D) return true; // double -> integer
        if (op >= OP_EQ && op <= OP_LE) return true; // FEQ FLT FLE
        return false;
    }

    /// @notice Hash of a state witness, for a party that has one in full.
    ///
    /// @dev    Exposed so callers can check a claimed nested state against the game's
    ///         accepted hash using the same encoding the VM uses.
    function stateHash(bytes calldata stateData) external pure returns (bytes32) {
        if (stateData.length != STATE_SIZE) revert BadStateLength();
        uint8 status = _vmStatus(uint8(stateData[OFF_EXITED]) == 1, uint8(stateData[OFF_EXIT_CODE]));
        return _withStatus(keccak256(stateData), status);
    }

    /// @notice Build the nested pre-state witness and hash it.
    ///
    /// @dev    Every field is zero except the memory root, the PC and `sp`. That is not an
    ///         oversight — the nested machine has no heap, no files, no preimage oracle and
    ///         no randomness, so an `mmap`, `openat` or `getrandom` inside the co-processor
    ///         would be a bug, and zeroing the cursors makes such a bug fail loudly rather
    ///         than diverge quietly between the two parties.
    function _preStateHash(bytes32 memRoot) internal view returns (bytes32) {
        bytes memory witness = new bytes(STATE_SIZE);

        assembly {
            let ptr := add(witness, 32)
            mstore(ptr, memRoot)
        }
        _writeU64(witness, OFF_PC, entry);
        _writeU64(witness, OFF_REGISTERS + 2 * 8, INITIAL_SP); // x2 = sp

        // Matches FileSystem.Commitment for a nested machine with filePointer=0, nextFd=100,
        // no staged files and no open descriptors. The digest is committed even though file
        // syscalls are forbidden inside the co-processor.
        bytes32 fileSystemRoot = keccak256(
            abi.encodePacked(
                "MLVM-FILESYSTEM-V2\x00",
                uint64(0),
                uint64(100),
                uint64(0),
                EMPTY_FILE_TREE_ROOT,
                EMPTY_FILE_TREE_ROOT
            )
        );
        assembly {
            mstore(add(add(witness, 32), OFF_FILE_SYSTEM_ROOT), fileSystemRoot)
        }

        // Not exited, so the status byte is "unfinished" — which is what it must be, since
        // the sub-game is about to execute from here.
        return _withStatus(keccak256(witness), VM_STATUS_UNFINISHED);
    }

    /// @notice Walk a merkle branch from a leaf up to a root.
    ///
    /// @dev    Mirrors `RISCV.sol`'s `getMemoryB32` / `setMemoryB32`: 59 sibling hashes above
    ///         a 32-byte leaf, with the path taken from `addr >> 5`. `IO_BASE` is a compile
    ///         time constant, so the path is too, and the direction bits fold away.
    function _reconstructRoot(bytes32 leaf, bytes calldata proof) internal pure returns (bytes32) {
        uint256 path = uint256(IO_BASE) >> 5;
        bytes32 node = leaf;
        for (uint256 i = 0; i < PROOF_LEN - 1; i++) {
            bytes32 sibling = bytes32(proof[(i + 1) * 32:(i + 2) * 32]);
            node = ((path >> i) & 1) == 0 ? _hashPair(node, sibling) : _hashPair(sibling, node);
        }
        return node;
    }

    /// @notice Walk two leaves up the same branch at once, sharing the sibling loads.
    function _reconstructBoth(bytes32 leafA, bytes32 leafB, bytes calldata proof)
        internal
        pure
        returns (bytes32, bytes32)
    {
        uint256 path = uint256(IO_BASE) >> 5;
        bytes32 nodeA = leafA;
        bytes32 nodeB = leafB;
        for (uint256 i = 0; i < PROOF_LEN - 1; i++) {
            bytes32 sibling = bytes32(proof[(i + 1) * 32:(i + 2) * 32]);
            if (((path >> i) & 1) == 0) {
                nodeA = _hashPair(nodeA, sibling);
                nodeB = _hashPair(nodeB, sibling);
            } else {
                nodeA = _hashPair(sibling, nodeA);
                nodeB = _hashPair(sibling, nodeB);
            }
        }
        return (nodeA, nodeB);
    }

    function _hashPair(bytes32 a, bytes32 b) internal pure returns (bytes32 h) {
        assembly {
            mstore(0x00, a)
            mstore(0x20, b)
            h := keccak256(0x00, 0x40)
        }
    }

    /// @notice Reverse the byte order of a 64-bit value.
    ///
    /// @dev    MLVM's memory is little-endian; the EVM reads big-endian words. Every operand
    ///         crossing this boundary needs the swap.
    function _le64(uint64 v) internal pure returns (uint64) {
        uint64 r = ((v & 0x00FF00FF00FF00FF) << 8) | ((v >> 8) & 0x00FF00FF00FF00FF);
        r = ((r & 0x0000FFFF0000FFFF) << 16) | ((r >> 16) & 0x0000FFFF0000FFFF);
        return (r << 32) | (r >> 32);
    }

    /// @notice Write a big-endian uint64 into a witness buffer.
    function _writeU64(bytes memory witness, uint256 offset, uint64 v) internal pure {
        for (uint256 i = 0; i < 8; i++) {
            witness[offset + i] = bytes1(uint8(v >> (8 * (7 - i))));
        }
    }

    /// @notice Replace the top byte of a hash with the VM status, as `StateWitness.StateHash`
    ///         does. It is what lets a dispute game read "did this machine halt cleanly"
    ///         straight off the committed hash.
    function _withStatus(bytes32 h, uint8 status) internal pure returns (bytes32) {
        return bytes32((uint256(h) & ~(uint256(0xff) << 248)) | (uint256(status) << 248));
    }

    function _vmStatus(bool exited, uint8 exitCode) internal pure returns (uint8) {
        if (!exited) return VM_STATUS_UNFINISHED;
        if (exitCode == 0) return VM_STATUS_VALID;
        if (exitCode == 1) return 1; // invalid
        return 2; // panic
    }
}
