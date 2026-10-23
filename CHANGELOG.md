# Changelog

All notable user-visible changes are documented here. MLVM follows Semantic Versioning;
before 1.0, a minor release may contain breaking changes.

## [Unreleased]

### Fixed

- An exact zero from a subtraction or fused multiply-add of opposite-signed values is now -0
  when rounding down (RDN), as IEEE 754 requires; it was +0 in every rounding mode. Programs
  that round to nearest, including every recorded model run, are unaffected.

## [0.2.0] - 2026-10-23

The first public release. Earlier 0.1 builds were private previews.

### Added

- A deterministic RV64IMAFD virtual machine in Go. Floating point runs through a Go port of
  Berkeley SoftFloat 3e, memory is a sparse Merkle tree over 2^64 bytes, staged files and
  descriptors live in authenticated trees, and a 674-byte state witness hashes to a 32-byte
  state root.
- `mlvm demo`: TinyStories-260K inside the VM, with the generated tokens committed in the
  final state root. Five prompts give identical roots on Linux, macOS, Windows and
  WebAssembly on every push.
- `mlvm liar`: a second machine changes one instruction's result (`--lie ulp`, `load`, `int`,
  `output`, `commit` or `token`). Bisection over state hashes finds that instruction, and it
  is checked by `RISCV.sol` in an in-process EVM, by its Go mirror `slow.Step`, or by
  SoftFloat re-execution. `--elf` plays the game on any static RV64IMAFD program.
- `mlvm referee`: rules on a finished dispute from its record (`mlvm.dispute-record/1`)
  without running either machine.
- `mlvm xray`: exact instruction-level profiles by function, call stack and instruction
  class, with flame graphs and folded stacks.
- A browser playground that runs the same VM as WebAssembly, and a technical write-up.
- Guests: C programs, the TinyStories decoder, and Rust guests through Candle, from
  TinyStories to BERT-family encoders and SmolLM2-135M (14.8 billion instructions).
- `mlvm-1` format specifications, release archives with checksums and build attestations,
  `install.sh`, and a Homebrew formula (`brew install yao-jz/tap/mlvm`).

### Known limitations

- There is no on-chain dispute game: no bonds, clocks or deployment. Float arithmetic is
  checked locally by SoftFloat; the nested `f64` sub-game is experimental and
  round-to-nearest-even only. MLVM has not been audited. See docs/STATUS.md.
