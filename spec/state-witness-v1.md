# State Witness v1

Format ID: `mlvm.state-witness/1`

Exact length: **674 bytes**. Integers are big-endian.

| Offset | Size | Field |
| ---: | ---: | --- |
| 0 | 32 | memory merkle root |
| 32 | 32 | preimage key |
| 64 | 8 | preimage offset |
| 72 | 8 | PC |
| 80 | 1 | exit code |
| 81 | 1 | exited (`0` or `1` only) |
| 82 | 8 | step |
| 90 | 8 | heap |
| 98 | 8 | load reservation |
| 106 | 256 | `x0..x31` |
| 362 | 256 | `f0..f31` |
| 618 | 8 | random base |
| 626 | 8 | random offset |
| 634 | 8 | `fcsr` |
| 642 | 32 | filesystem root |

State hash = `keccak256(witness)` with byte 0 replaced by VM status
(`0` valid, `1` invalid, `2` panic, `3` unfinished).

Non-674 lengths and non-canonical `exited` values MUST be rejected by every consumer.

Current implementation: the fast Go `StateWitness.Validate` rejects both. `slow.Step` and
`RISCV.sol` reject a state that is not 674 bytes, but currently treat any non-zero `exited`
byte as exited rather than rejecting values other than `0` and `1`.
