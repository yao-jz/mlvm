# Filesystem Proof v2

Format ID: `mlvm.filesystem-proof/2`

Exact length: **992 bytes** (31 × 32-byte words). Domain separation strings:

- `MLVM-FILESYSTEM-V2\0`
- `MLVM-FILE-V2\0`
- `MLVM-FD-V2\0`

Limits: 1,024 leaves, fd 100..1023, path ≤255 bytes, read ≤32 bytes.
The descriptor tree index is supplied out of band by the syscall fd.
