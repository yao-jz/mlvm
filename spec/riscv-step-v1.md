# RISCV Step ABI v1

Format ID: `mlvm.riscv-step/1`

```solidity
function step(bytes calldata stateData, bytes calldata proof, bytes32 localContext)
    external returns (bytes32 postStateHash);
```

- Selector: `0xe14ced32`
- `stateData` length: exactly 674
- `proof`: concatenated 1,920-byte memory branches, optional 992-byte filesystem trailer
- Return: state hash with status byte
- Side effect: `log0` emits the full post-state witness

Contracts SHOULD also expose `oracle()` (Optimism `IBigStepper`) and a `semanticsId()`
returning `keccak256("mlvm.riscv-step/1")`.

Current implementation: `RISCV.sol` has the `step` signature above but exposes
`preimageOracle()` rather than `oracle()`, and no `semanticsId()`; `MLDisputeGame.sol`
stores a `semanticsId` passed to its constructor. `stateData` of any other length is
rejected.
