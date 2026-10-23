# State Snapshot Schema

MLVM snapshots are reconstruction artifacts: they contain enough data to resume the fast
VM without reopening the guest ELF or staged host files. They are not the same object as
the compact binary state witness used by a single-step verifier.

The current top-level JSON format is **schema version 3** (`mlvm.snapshot/3`, specified in
[spec/snapshot-v3.md](../spec/snapshot-v3.md)). An abridged example:

```json
{
  "schemaVersion": 3,
  "memory": [],
  "pc": 0,
  "registers": [],
  "fileSystem": {
    "version": 2,
    "files": [],
    "descriptors": [],
    "nextFd": 100
  }
}
```

A real version 3 snapshot must contain every consensus field — `schemaVersion`, `memory`,
`preimageKey`, `preimageOffset`, `pc`, `exit`, `exited`, `step`, `heap`, `loadReservation`,
`registers`, `fregisters`, `fileSystem`, `filePointer`, `randomFileAddr`,
`randomFileOffset` and `fcsr` — and unknown fields are rejected; `lastHint` and `opCount`
are optional. Validation also requires non-zero `heap`, `randomFileAddr` and `filePointer`.

`mlvm load-elf` and snapshots emitted by `mlvm run` always write version 3. `mlvm run` and
`mlvm witness` validate the version before using the state and accept versions 2 and 3;
version 2 is still read without the strict field checks, for prerelease fixtures. A missing
version is not treated as a current version: older snapshots can otherwise acquire zero
values for newer consensus-relevant fields without an error.

## Version Surfaces

MLVM has three related but independent formats:

| Surface | Current version | Purpose |
| --- | ---: | --- |
| JSON snapshot | `schemaVersion: 3` (2 still readable) | Reconstruct memory pages and host-side VM metadata. |
| File-system metadata | `fileSystem.version: 2` | Serialize the staged-file manifest and descriptors. |
| Binary witness | 674 bytes, pre-1.0 | Commit the architectural state checked by one-step verifiers. |

Changing one does not automatically change the others. For example, adding optional
diagnostic metadata to snapshots need not alter a witness. Adding architectural state does.

## Fields

Versions 2 and 3 store the same fields; version 3 makes the consensus fields mandatory. They
store sparse memory pages, pre-image state, integer and floating-point registers,
PC, exit state, step count, heap and reservation state, deterministic-randomness location
and cursor, `fcsr`, the staged-file allocation cursor, and the complete staged-file manifest
and descriptor table. `lastHint` and `opCount` are execution metadata; they are serialized
but are not part of the binary state commitment.

File bytes and deterministic randomness bytes are stored in sparse memory pages. A snapshot
does not retain paths to the original host files and resuming it must not read them again.

## Compatibility Policy

MLVM is pre-1.0. A release may introduce a new snapshot or witness format, but it must:

1. increment the affected explicit version or document a new witness size;
2. add a changelog compatibility note;
3. preserve or intentionally replace the checked-in compatibility fixture; and
4. update every Go, Rust, and Solidity consumer of the affected bytes.

There is currently no migration path for unversioned snapshots. Regenerate them from the
guest ELF and deterministic inputs with the current `mlvm load-elf`.

The compact fixture at
[`rvgo/fast/testdata/state-v2.json`](../rvgo/fast/testdata/state-v2.json)
contains non-zero integer, floating-point, randomness, and file-descriptor state. Its test
locks the 674-byte witness size plus the resulting state, memory, and file-system hashes.
It is a version 2 snapshot; there is no checked-in version 3 fixture yet.
