# Contributing To MLVM

MLVM welcomes focused contributions to deterministic execution, RISC-V support, proof
tooling, developer experience, tests, and documentation. Consensus-sensitive changes need a
clear correctness argument in addition to working code.

By participating, you agree to follow [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Start Here

```bash
git clone https://github.com/yao-jz/mlvm.git
cd mlvm
make doctor
make check
make build
```

The core workflow needs Go 1.23+. A RISC-V cross compiler is needed for guest changes;
Foundry is needed for contracts; Rust nightly is needed for the nested co-processor and
Candle guests. See [docs/INSTALL.md](../docs/INSTALL.md).

Before implementing a large change, open an issue. This is required for state-layout,
witness, syscall, ISA, and contract-boundary changes because those decisions affect more
than one implementation.

## Good Contribution Areas

- Minimal bug reproductions for incorrect or nondeterministic transitions.
- Differential tests across fast, slow, EVM, SoftFloat, and native references.
- Completing the committed file-descriptor state.
- Generating a tested syscall-equivalence matrix.
- Single-precision nested settlement.
- State/witness schema versioning and compatibility fixtures.
- CLI inspection, profiling, and machine-readable output.
- Additional `f32`/`f64` placement benchmarks with repeated measurements.
- Installation fixes verified on a clean operating system.
- Documentation that replaces an assumption with a reproducible command.

The milestone ordering in [ROADMAP.md](../docs/ROADMAP.md) is a stronger prioritization signal than
the number of unchecked boxes in older research notes.

## Invariants

### Determinism

Two parties with the same committed inputs must observe identical state transitions. Host
time, randomness, CPU features, filesystem metadata, thread scheduling, uninitialized data,
and implicit library behavior are all suspect inputs.

### Three implementations, one transition

Fast Go execution generates the trace. Slow Go execution models the verifier. Solidity is
the adjudicator. A behavior implemented only in fast mode is executable but not settleable.
Pull requests must state which surfaces implement the change.

### Complete committed state

Any value that can affect a transition must either be in the witness, derivable from
committed data, or explicitly excluded from settleable execution. Adding convenient
host-side state without a commitment design is not acceptable.

### Fail explicitly

Unsupported instructions and syscalls should fail rather than silently return a plausible
result. Deterministic wrong answers are still wrong and are harder to detect than failures.

### Measurements are artifacts

Do not edit benchmark numbers by hand to match an expectation. Rerun the documented command,
record the environment and repetitions, commit the generated summary, and explain material
changes.

## Test Matrix

Run `make check` for every pull request. Then select the additional rows relevant to the
change:

| Change | Required verification |
| --- | --- |
| Go implementation or public package | `make check` |
| Instruction semantics | `make test-fd` when applicable, differential oracle tests, `make isa` |
| Witness or Merkle memory | full `go test ./...`, compatibility fixture update |
| Slow/EVM semantics | `make test-all` |
| Solidity contract | `make test-sol` and the matching Go/EVM integration test |
| Float sub-game | `make float-subgame` and `make float-dispute` |
| Guest/runtime/syscall | relevant example plus fast/slow/EVM equivalence evidence |
| Performance claim | benchmark command with repeated-run metadata |
| CLI or release | `make build`, `bin/mlvm --version`, and a snapshot release build |

Some integration tests require generated Foundry artifacts. `make test-all` builds those
before running the Go EVM checks.

## Pull Requests

Keep a pull request focused on one observable behavior. Include:

- the problem and intended behavior;
- a minimal test that fails before the change;
- determinism, state, proof, and compatibility impact;
- commands used to verify the result;
- documentation and changelog updates when users are affected.

Generated files should be committed only when the repository intentionally treats them as
fixtures or result artifacts. Avoid unrelated formatting, dependency, or benchmark churn.

## Code And Documentation Style

- Run `make fmt`; CI rejects unformatted Go files.
- Use Go errors with enough context to locate the failing guest step or artifact.
- Comment protocol invariants and non-obvious bit manipulations, not routine assignments.
- Use repository-relative commands in documentation.
- Label capabilities using the vocabulary in [docs/STATUS.md](../docs/STATUS.md).
- Preserve upstream attribution when moving or substantially editing derived code.

## Security Issues

Do not file exploitable consensus or verifier bugs publicly before a fix is available. Follow
[SECURITY.md](SECURITY.md) for private reporting.

## Upstream Relationship

MLVM is derived from [ethereum-optimism/asterisc](https://github.com/ethereum-optimism/asterisc).
General fixes to inherited interpreter or Merkle-memory behavior may also belong upstream.
MLVM-specific floating-point, workload, state-extension, and proof experiments should remain
clearly attributable in this repository.
