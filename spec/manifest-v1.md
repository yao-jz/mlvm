# Execution Manifest v1

`manifestVersion: 1`

A manifest binds the exact bytes of a guest ELF and its staged inputs to an expected
prestate hash. Host paths are not part of the canonical digest.

```json
{
  "manifestVersion": 1,
  "profile": "mlvm-1",
  "executionProfile": "mlvm-1",
  "formats": {
    "snapshot": "mlvm.snapshot/3",
    "stateWitness": "mlvm.state-witness/1",
    "filesystemProof": "mlvm.filesystem-proof/2",
    "riscvStep": "mlvm.riscv-step/1"
  },
  "mlvm": { "version": "0.1.0", "commit": "…" },
  "guest": { "sha256": "…", "size": 0 },
  "inputs": [
    { "guestPath": "model.bin", "sha256": "…", "size": 0 }
  ],
  "randomness": { "mode": "zero", "size": 5242880 },
  "expectedPrestate": "0x…"
}
```

## Rules

1. `guestPath` values are compared verbatim by the guest `openat`.
2. Reordering `inputs` changes the filesystem root and therefore the prestate.
3. A host `sourcePath` may appear for local resolution but MUST NOT enter the digest.
4. Any digest or size mismatch rejects the load before execution.
