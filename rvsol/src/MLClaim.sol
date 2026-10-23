// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;

/// @title MLClaim
/// @notice Reads a result commitment from an MLVM state witness.
///
///         A guest writes its result into memory at a fixed absolute address
///         (`examples/mlvm_result.h`). The state's memory root then commits to those bytes,
///         unlike stdout, which is not part of the state witness.
///
///         This contract validates a branch against the memory root supplied inside a halted
///         state witness and reads the result bytes. A proposed claim can identify
///
///             (preState, postState, answer)
///
///         but a surrounding protocol must establish which prestate and poststate are valid.
///         `readAnswer` does not prove that execution reached the caller-supplied witness.
///         Staged-file metadata is committed by digest, but staged-file syscall transitions
///         are not implemented by the on-chain stepper; see `docs/STATUS.md`.
contract MLClaim {
    /// @notice Where a guest writes its answer. 64-byte aligned, so the two 32-byte merkle
    ///         leaves covering the window are siblings and one branch carries both:
    ///
    ///             +0   magic ("MLVM_OUT")   +8  length      <- leaf, proof[0]
    ///             +16  payload[0..16]
    ///             +32  payload[16..48]                      <- sibling, proof[1]
    uint64 public constant RESULT_BASE = 0x3100000000000000;

    /// @notice "MLVM_OUT". Without it an untouched page — which reads as zeros, and so as a
    ///         zero-length answer — would be indistinguishable from a real commitment.
    uint64 public constant RESULT_MAGIC = 0x4D4C564D5F4F5554;

    /// @notice Payload capacity: 64 bytes of window minus the 16-byte header.
    uint256 public constant RESULT_MAX = 48;

    uint256 internal constant STATE_SIZE = 674;
    uint256 internal constant PROOF_LEN = 60;
    uint256 internal constant OFF_MEM_ROOT = 0;
    uint256 internal constant OFF_EXIT_CODE = 80;
    uint256 internal constant OFF_EXITED = 81;

    error BadStateLength();
    error BadProofLength();
    error MemProofMismatch();
    error MachineNotHalted();
    error MachineFaulted(uint8 exitCode);
    error NoResultCommitted();
    error ResultTooLong(uint64 length);

    /// @notice Read the answer a guest committed, out of the state it halted in.
    ///
    ///         Insists the machine halted cleanly first. A run that faulted, or one that was
    ///         cut short, is not a completed guest result — and its result window may hold a
    ///         partially written answer from a previous step.
    ///
    /// @param stateData The final state witness, 674 bytes.
    /// @param proof     Merkle branch for `RESULT_BASE` against that state's memory root.
    /// @return answer   The committed bytes, exactly as the guest wrote them.
    function readAnswer(bytes calldata stateData, bytes calldata proof)
        external
        pure
        returns (bytes memory answer)
    {
        if (stateData.length != STATE_SIZE) revert BadStateLength();
        if (proof.length != PROOF_LEN * 32) revert BadProofLength();

        if (uint8(stateData[OFF_EXITED]) != 1) revert MachineNotHalted();
        uint8 exitCode = uint8(stateData[OFF_EXIT_CODE]);
        if (exitCode != 0) revert MachineFaulted(exitCode);

        bytes32 memRoot = bytes32(stateData[OFF_MEM_ROOT:OFF_MEM_ROOT + 32]);
        bytes32 leaf0 = bytes32(proof[0:32]);
        if (_reconstructRoot(leaf0, proof) != memRoot) revert MemProofMismatch();

        // The header lives in the most significant bytes of the leaf, and the VM stores its
        // words little-endian.
        uint256 w0 = uint256(leaf0);
        if (_le64(uint64(w0 >> 192)) != RESULT_MAGIC) revert NoResultCommitted();
        uint64 length = _le64(uint64(w0 >> 128));
        if (length > RESULT_MAX) revert ResultTooLong(length);

        // The payload spans both leaves: the low 16 bytes of this one, then all 32 of its
        // sibling, which the branch above already authenticated as `proof[1]`.
        bytes memory window = abi.encodePacked(bytes16(uint128(w0)), proof[32:64]);
        answer = new bytes(length);
        for (uint256 i = 0; i < length; i++) {
            answer[i] = window[i];
        }
    }

    /// @notice An identifier binding a proposed prestate, poststate, and answer together.
    ///
    /// @dev    This function only computes an identifier. A surrounding protocol must
    ///         independently establish the valid poststate and verify that `readAnswer`
    ///         returns `answer`; this contract does not settle that protocol.
    function claimId(bytes32 preState, bytes32 postState, bytes calldata answer)
        external
        pure
        returns (bytes32)
    {
        return keccak256(abi.encode(preState, postState, keccak256(answer)));
    }

    /// @notice Walk a merkle branch from a leaf up to a root.
    ///
    /// @dev    Mirrors `RISCV.sol`'s `getMemoryB32`: 59 sibling hashes above a 32-byte leaf,
    ///         with the path taken from `addr >> 5`. `RESULT_BASE` is a compile-time constant,
    ///         so the path is too.
    function _reconstructRoot(bytes32 leaf, bytes calldata proof) internal pure returns (bytes32) {
        uint256 path = uint256(RESULT_BASE) >> 5;
        bytes32 node = leaf;
        for (uint256 i = 0; i < PROOF_LEN - 1; i++) {
            bytes32 sibling = bytes32(proof[(i + 1) * 32:(i + 2) * 32]);
            node = ((path >> i) & 1) == 0 ? _hashPair(node, sibling) : _hashPair(sibling, node);
        }
        return node;
    }

    function _hashPair(bytes32 a, bytes32 b) internal pure returns (bytes32 h) {
        assembly {
            mstore(0x00, a)
            mstore(0x20, b)
            h := keccak256(0x00, 0x40)
        }
    }

    /// @notice Reverse the byte order of a 64-bit value. MLVM's memory is little-endian; the
    ///         EVM reads big-endian words.
    function _le64(uint64 v) internal pure returns (uint64) {
        uint64 r = ((v & 0x00FF00FF00FF00FF) << 8) | ((v >> 8) & 0x00FF00FF00FF00FF);
        r = ((r & 0x0000FFFF0000FFFF) << 16) | ((r >> 16) & 0x0000FFFF0000FFFF);
        return (r << 32) | (r >> 32);
    }
}
