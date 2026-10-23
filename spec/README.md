# MLVM-1 Protocol Specifications

This directory freezes the wire formats of the `mlvm-1` execution profile.

| Document | Format ID | Status |
| --- | --- | --- |
| [manifest-v1.md](manifest-v1.md) | `mlvm.manifest/1` | Stable for v0.1 |
| [snapshot-v3.md](snapshot-v3.md) | `mlvm.snapshot/3` | Stable for v0.1 |
| [state-witness-v1.md](state-witness-v1.md) | `mlvm.state-witness/1` | Stable (674 bytes) |
| [filesystem-proof-v2.md](filesystem-proof-v2.md) | `mlvm.filesystem-proof/2` | Stable (992 bytes) |
| [riscv-step-v1.md](riscv-step-v1.md) | `mlvm.riscv-step/1` | Stable ABI |
| [decoder-result-v1.md](decoder-result-v1.md) | `mlvm.decoder-result/1` | Stable for demo |
| [dispute-v0.md](dispute-v0.md) | `mlvm.dispute/0` | Experimental |
| [dispute-record-v1.md](dispute-record-v1.md) | `mlvm.dispute-record/1` | Experimental |

Breaking changes must introduce a new profile ID. The `mlvm-1` profile is never reinterpreted.

"Stable" here means stable within the `mlvm-1` profile. MLVM itself is pre-1.0 and
[docs/STATUS.md](../docs/STATUS.md) does not treat the witness format as stable before 1.0:
a later release may define a new profile instead of keeping these formats. Where a document
below says MUST or SHOULD, its "Current implementation" note, if any, records where the code
does not yet comply.
