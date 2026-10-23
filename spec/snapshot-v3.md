# Snapshot Schema v3

`schemaVersion: 3`

Version 3 is a strict evolution of the repository's v2 snapshots:

- every consensus field is required
- unknown JSON fields are rejected
- `exited` must be the boolean `true`/`false` (encoded 0/1 in the witness)
- `fileSystem.version` remains `2`

Readers that only accept `schemaVersion: 2` remain supported for prerelease artifacts
during the v0.1 cycle, but `mlvm-1` manifests require v3.
