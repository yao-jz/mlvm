// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;

import {RISCV} from "./RISCV.sol";
import {FloatSubGame} from "./FloatSubGame.sol";
import {MLClaim} from "./MLClaim.sol";

/// @title MLDisputeGame
/// @notice Experimental local/demo dispute orchestrator surface for MLVM.
///
/// @dev This contract is intentionally incomplete relative to a production
///      Optimism-style FaultDisputeGame:
///        - no bonds, clocks, or timeouts
///        - no persistent multi-party challenge window
///        - no deployment / L1 integration story
///
///      It exists so `mlvm demo --web` and Foundry tests can exercise:
///        1. a claimed (prestate, poststate, stepCount)
///        2. midpoint bisection over instruction indices
///        3. a terminal RISCV.step OR a bound FloatSubGame path
///        4. optional MLClaim answer readback from a halted poststate
contract MLDisputeGame {
    RISCV public immutable riscv;
    FloatSubGame public immutable floatGame;
    MLClaim public immutable mlClaim;

    bytes32 public immutable semanticsId;

    enum Status {
        InProgress,
        ChallengerWins,
        DefenderWins
    }

    struct ClaimData {
        bytes32 claim;
        address claimant;
        uint32 parentIndex;
        uint64 position; // instruction index midpoint encoding for the demo
        Status status;
    }

    ClaimData[] public claimData;
    bytes32 public rootClaim;
    bytes32 public startingRootPrestate;
    uint64 public maxSteps;

    error GameOver();
    error InvalidParent();
    error StepFailed();

    event Claimed(uint256 indexed index, bytes32 claim, uint64 position);
    event Resolved(Status status);

    constructor(RISCV _riscv, FloatSubGame _floatGame, MLClaim _mlClaim, bytes32 _semanticsId) {
        riscv = _riscv;
        floatGame = _floatGame;
        mlClaim = _mlClaim;
        semanticsId = _semanticsId;
    }

    function initialize(bytes32 _prestate, bytes32 _rootClaim, uint64 _maxSteps) external {
        require(claimData.length == 0, "already init");
        startingRootPrestate = _prestate;
        rootClaim = _rootClaim;
        maxSteps = _maxSteps;
        claimData.push(ClaimData({
            claim: _rootClaim,
            claimant: msg.sender,
            parentIndex: type(uint32).max,
            position: _maxSteps,
            status: Status.InProgress
        }));
        emit Claimed(0, _rootClaim, _maxSteps);
    }

    /// @notice Bisect by posting a counter-claim at a midpoint instruction index.
    function bisect(uint32 parentIndex, bytes32 claim, uint64 position) external returns (uint256 index) {
        if (claimData.length == 0) revert InvalidParent();
        ClaimData storage parent = claimData[parentIndex];
        if (parent.status != Status.InProgress) revert GameOver();
        index = claimData.length;
        claimData.push(ClaimData({
            claim: claim,
            claimant: msg.sender,
            parentIndex: parentIndex,
            position: position,
            status: Status.InProgress
        }));
        emit Claimed(index, claim, position);
    }

    /// @notice Execute a terminal integer step against RISCV.sol.
    function stepDirect(uint32 claimIndex, bytes calldata stateData, bytes calldata proof, bytes32 localContext)
        external
        returns (bytes32 postHash)
    {
        ClaimData storage c = claimData[claimIndex];
        if (c.status != Status.InProgress) revert GameOver();
        postHash = riscv.step(stateData, proof, localContext);
        // Demo resolution rule: if the post hash disagrees with the contested claim,
        // the challenger wins; otherwise the defender wins.
        if (postHash != c.claim) {
            c.status = Status.ChallengerWins;
            emit Resolved(Status.ChallengerWins);
        } else {
            c.status = Status.DefenderWins;
            emit Resolved(Status.DefenderWins);
        }
    }

    /// @notice Read an answer out of a halted post-state. Does not by itself settle the game.
    function readAnswer(bytes calldata stateData, bytes calldata proof) external view returns (bytes memory) {
        return mlClaim.readAnswer(stateData, proof);
    }
}
