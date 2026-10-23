# Roadmap

MLVM is being developed as a deterministic RISC-V execution platform first and a
fraud-proof research platform second. The order matters: proof integrations are meaningful
only when the state and transition boundaries are complete.

Dates are intentionally omitted. Milestones are capability gates, not promises.

## 0.1: Open-Source Foundation

- [x] Move the Go module to `github.com/yao-jz/mlvm`.
- [x] Publish a first-class `mlvm` command with build version information.
- [x] Define tested, experimental, and planned capability levels.
- [x] Add repeatable maintainer checks and cross-platform release automation.
- [x] Add contribution, governance, security, and changelog policies.
- [ ] Publish the first signed/attested release and verify installation on Linux and macOS.
- [x] Add a small compatibility fixture for the JSON state and witness encodings.
- [x] Ship a one-command `mlvm demo` with embedded guest + receipt.
- [x] Land a TinyStories decoder guest and pinned model-pack manifest.
- [x] Publish the `mlvm-1` / `spec/` format anchors (manifest, witness, step ABI).

## 0.2: Complete Deterministic State

- [x] Commit file-descriptor mappings, offsets, and allocation state, or derive them
  canonically from committed inputs.
- [x] Define a versioned machine-state schema.
- [x] Make bounded staged-file syscalls agree in fast, slow, and Solidity mode.
- [ ] Make randomness and every remaining evaluated syscall agree in all verifier modes.
- [ ] Generate a syscall support matrix from executable tests.
- [ ] Add trace-level differential testing that samples transitions from complete guests.
- [ ] Add deterministic input manifests so a prestate can be rebuilt from content hashes.

Exit criterion: every transition used by the standard C examples can be reproduced from
committed state alone in both Go implementations.

## 0.3: Complete Float Settlement Prototype

- [ ] Add nested single-precision operations and NaN-boxed result handling.
- [ ] Bind the disputed outer instruction and operands to the nested prestate on chain.
- [ ] Bind the nested result and `fflags` to the required outer post-state.
- [ ] Test valid adversarial traces rather than synthetic hash corruption.
- [ ] Measure calldata, bisection, game bookkeeping, and terminal execution separately.
- [ ] Pin the EVM fork, Solidity compiler, co-processor toolchain, and program image.

Exit criterion: an `f32` and an `f64` arithmetic transition can be challenged through one
complete experimental game path with no caller-supplied state left unbound.

## 0.4: Developer Platform

- [ ] Stabilize an embeddable Go API for loading, stepping, profiling, and proving guests.
- [ ] Add machine-readable `inspect`, `profile`, and state-schema commands.
- [ ] Publish minimal container images and reproducible guest toolchains.
- [ ] Provide versioned examples for C, Rust `no_std`, and Rust `std`.
- [x] Commit the Candle BERT guest's score at `MLVM_OUT`, as the TinyStories guest commits
  its tokens, so that a forged score is disputable (see
  [GALLERY](GALLERY.md#printing-is-not-committing)).
- [ ] Add benchmark result schemas with repetitions and environment metadata.
- [ ] Document extension points for new ISA operations and deterministic devices.

Exit criterion: an external project can embed MLVM without importing internal command code
or depending on repository-relative files.

## 1.0: Stable Verification Substrate

- [ ] Freeze and document compatibility guarantees for state, witness, CLI, and contract ABIs.
- [ ] Complete the evaluated syscall equivalence surface.
- [ ] Integrate or provide a complete dispute-game adapter.
- [ ] Commission an independent security review of consensus-critical code.
- [ ] Resolve all high-severity findings and publish the review.
- [ ] Maintain multiple external users or integrations through a release cycle.
- [ ] Provide migration tooling for every supported pre-1.0 state format retained at 1.0.

## Non-Goals

- General-purpose Linux emulation.
- Nondeterministic threads or access to host devices.
- Replacing native inference on the honest path.
- Claiming cryptographic proof from ordinary deterministic execution alone.
- Expanding ISA coverage without a test and proof-surface story.
