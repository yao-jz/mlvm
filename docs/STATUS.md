# Project Status

This document defines MLVM's trust and maturity boundary. It is the source of truth when a
README, example, or research note uses words such as "supported", "verified", or
"end-to-end".

MLVM is pre-1.0 research software. Its fast interpreter is useful independently; its proof
components are not a production dispute system.

## Maturity Labels

| Label | Meaning |
| --- | --- |
| **Tested** | The behavior has automated tests in the root CI workflow and is expected to remain working. |
| **Experimental** | An implementation exists, but its security boundary, integration, or compatibility is incomplete. |
| **Workload demo** | The code demonstrates execution or measurement, not a supported verification path. |
| **Planned** | Design or research notes may exist, but users cannot rely on an implementation. |

"Tested" is not the same as audited, formally verified, stable, or production-ready.

## Execution Core

| Component | Status | Evidence | Boundary |
| --- | --- | --- | --- |
| RV64I/M/A fast execution | **Tested** | 84 integer, multiply, and atomic conformance suites | RVC, privileged mode, and virtual memory are excluded. |
| RV64F/D fast execution | **Tested** | 23 applicable floating-point suites plus differential tests | Host libm behavior is outside the ISA and is not normalized. |
| Floating-point state | **Tested** | Float registers and `fcsr` are encoded in the 674-byte witness | Witness format is not stable before 1.0. |
| Sparse Merkle memory | **Tested** | Page/tree tests and proof reconstruction tests | The memory root commits mapped bytes. |
| State snapshots | **Tested, pre-1.0** | Version validation and v2 compatibility fixture | Unversioned and v1 snapshots are rejected; no migration tool exists yet. |
| Staged-file transitions | **Tested** | Authentication, tamper, fast/slow, and embedded-EVM tests | Read-only; 1,024 files/fds and 32 bytes per read. |
| C/Rust runtime surface | **Tested for examples** | CI boots and runs the checked-in static guests | This is not a complete Linux ABI. |
| Threads/network/dynamic linking | **Not supported** | N/A | These are explicit non-goals for the deterministic core. |

## Proof Surface

| Component | Status | Evidence | Boundary |
| --- | --- | --- | --- |
| Single-step witness generation | **Tested** | Fast VM witness tests and proof demo | A witness supplies the data needed to check one supported transition; it is not an entire-computation proof. |
| Slow Go state oracle | **Experimental** | Cross-implementation instruction tests | Mirrors 22 of the 35 syscalls the fast VM answers; `getrandom` and 12 others revert, a whole-trace gap. |
| `RISCV.sol` integer stepper | **Experimental** | Embedded-EVM conformance tests | It is unaudited and not distributed as a deployed protocol. Mirrors 27 of the 35 answered syscalls; `getrandom` is a no-op there. |
| Bit-manipulation float instructions | **Experimental** | Fast/slow/EVM equality tests | Coverage must be checked instruction by instruction. |
| Arithmetic `f64` sub-game | **Experimental** | Nested VM, boundary contract, and EVM terminal-step tests | Round-to-nearest-even only; no production outer game; costs shown by tests are component costs. |
| Arithmetic `f32` sub-game | **Experimental / incomplete** | Outer binding helpers + op decode exist; nested f32 image pending | Decoder traces are majority `f32`; public demo stays honest about this gap. |
| `MLClaim.sol` answer readback | **Experimental** | Contract and Go integration tests | It binds data to a post-state; it does not prove that the post-state won a dispute. |
| One-command `mlvm demo` (embedded TinyStories) | **Tested** | CI guests job + receipt Verify() | Fast execution + result commitment; not a full-trace proof. |
| TinyStories-260K decoder guest | **Tested (fast path)** | Host/VM token identity (16 greedy tokens) | f32 arithmetic still needs nested settlement for full-trace claims. |
| Embedded MLP fallback (`--demo mlp`) | **Tested** | CI + slow-step verify | Ships in the same binary. |
| Outer float operand binding | **Experimental** | `BindFloatInstruction` unit tests | Required before FloatSubGame is sound. |
| `MLDisputeGame.sol` + Go `internal/dispute` | **Experimental** | Unit tests + `mlvm demo --web` tamper act | No bonds/timeouts; local bisection / forged-poststate demo only. |
| Cross-platform state roots | **Tested** | `determinism.yml` runs five decoder prompts on linux/amd64, linux/arm64, darwin/arm64, darwin/amd64, windows/amd64 and WebAssembly and requires identical roots | Covers the decoder demo; other guests are compared with native builds by `make determinism`, not across hosts. |
| `mlvm liar` dispute simulation | **Tested (local simulation)** | `internal/liar` tests: bisection lands on the deviating step for f32 and f64 ULP lies, one-bit float-load and integer lies, forged stdout, forged committed answers and forged tokens; the referee rejects a witness that does not hash to the agreed state. Every test dispute is ruled on again from its JSON record (`mlvm referee`), and records with a changed question, answer, witness byte or proof sibling are rejected. Runs on the embedded guests and any `--elf`, including Candle BERT ([GALLERY](GALLERY.md)) | No chain, bonds or clocks. Integer and memory verdicts (float loads and stores included) use `slow.Step` and `RISCV.sol` bytecode in an in-process EVM; f32 and f64 arithmetic verdicts use SoftFloat re-execution from the witness, which is not settleable on chain. |
| `mlvm xray` profiler | **Tested** | Profiled and unprofiled poststates match; `internal/rvdis` agrees with GNU objdump on both embedded guests | Counts instructions, not time. Stacks come from a shadow stack (calls link through `ra`/`t0`, `ret` pops). |
| Browser playground (WebAssembly) | **Tested** | `web/test/protocol.mjs` plays a full dispute across three instances in CI | Omits the EVM; about 90 MB per party; a local demonstration. |
| Embedded `RISCV.sol` bytecode | **Experimental** | `internal/evmstep` cross-checks against `slow.Step` and fails if it drifts from the forge artifact | The same unaudited contract, under London rules, without a pre-image oracle. |
| Complete interactive dispute game | **Planned** | None | Bonds, timeouts, challenger services, and deployment are absent. |

## Known Boundaries and Soundness Gaps

### Staged-file boundary

Staged file bytes are committed in VM memory. File names, addresses, sizes, the allocation
cursor, open descriptors, and their offsets are serialized in snapshots and bound by the
final 32 bytes of the witness. Changing any of that metadata changes the state hash.

File names are limited to 255 bytes and reads to 32 bytes. A fixed-depth file tree and
fd-indexed descriptor tree add a 992-byte proof to `openat`, staged `read`, `fcntl`, `lseek`,
and `close`. The slow interpreter and `RISCV.sol` authenticate those branches and derive the
same post-state as fast mode. Files remain immutable, the trees cap staged files and fds at
1,024, and `stat`/`statx` are compatibility stubs rather than authenticated metadata APIs.

### Syscall equivalence

The fast interpreter still has a wider syscall implementation than the slow interpreter
and `RISCV.sol`, and the two verifiers do not match each other either. `getrandom` serves
committed bytes in fast mode, reverts in the slow interpreter, and is a no-op that returns
the requested count in `RISCV.sol`. Several start-up stubs (`readlinkat`, `stat`,
`set_tid_address`, `get_robust_list`, `mprotect`, `prlimit64`, and others) are missing from
one or both verifiers. The five C examples and the Rust BERT guest, traced on 2026-10-16,
all call some of them, so none of those complete traces can yet be replayed by either
verifier. The per-syscall table is
in [SYSCALLS.md](SYSCALLS.md#coverage-outside-the-fast-vm).

An executable syscall is not automatically a provable syscall.

### Float sub-game binding

`FloatSubGame.sol` derives and reads nested states. It is not an outer dispute contract and
does not by itself establish that caller-supplied operands came from a particular disputed
outer instruction. Any integration must verify that binding and apply the result and flags
to the correct outer post-state.

The nested machine always computes in round-to-nearest-even, and neither its operand ABI nor
`DecodeFloatSubGame` carries the outer instruction's rounding mode. The fast VM honours
static `rm` values and dynamic `frm`, so an `f64` instruction that rounds in any other mode
would be settled to a different result. Such instructions occur in practice: glibc's
`expm1`, reached from the C MLP's `tanh`, executes `fcvt.w.d ..., rtz`.

### Model scope

The checked-in transformer measurements use the fast VM and demonstrate ISA demand and
runtime scale. They are not end-to-end fraud-proof demonstrations because their traces use
single-precision arithmetic and there is no complete outer dispute game.

## Security Assumptions

The current experiments assume:

- all parties agree on the guest ELF, staged inputs, and deterministic randomness input;
- Keccak-256 behaves as a collision-resistant hash for state commitments;
- the fast, slow, and Solidity implementations agree for the transition being checked;
- the compiler and co-processor images used by nested experiments are fixed and inspected;
- the host executing the fast VM is untrusted, while the adjudicator is authoritative.

Those assumptions have not been reviewed by an independent security audit.

## Release Readiness

A tagged release means that binaries and documentation were built from a specific commit.
It does not promote experimental components to production status.

The requirements for a stable 1.0 release are listed in [ROADMAP.md](ROADMAP.md). Any
change that alters this status matrix must update this file in the same pull request.
