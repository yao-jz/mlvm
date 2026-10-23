# The OS surface

MLVM has no kernel. `ECALL` traps into a switch on register `a7` in
[`rvgo/fast/vm.go`](../rvgo/fast/vm.go), which recognises **37 Linux syscall
numbers**: 35 return a result (many of them fixed stubs) and 2 revert as explicitly
unsupported. That is enough for a statically linked glibc program or a Rust `std` binary to
start up, allocate memory, read files and print. The slow Go interpreter and `RISCV.sol`
implement a smaller, not identical, subset; see
[Coverage outside the fast VM](#coverage-outside-the-fast-vm).

Everything a real kernel would answer with host state is either refused or replaced with a
committed constant, because a fraud proof over an execution that depended on the machine
it ran on would be worthless.

---

## Contents

- [Memory map](#memory-map)
- [The syscall table](#the-syscall-table)
- [Calling convention](#calling-convention)
- [File descriptors](#file-descriptors)
- [In-memory file system](#in-memory-file-system)
- [Randomness](#randomness)
- [The pre-image oracle](#the-pre-image-oracle)
- [What is and is not proven](#what-is-and-is-not-proven)
- [Coverage outside the fast VM](#coverage-outside-the-fast-vm)
- [Adding a syscall](#adding-a-syscall)

---

## Memory map

![guest address space](fig/address-space.svg)

| Base | Region | Notes |
| --- | --- | --- |
| `0x0000_0000_0000_0000` | ELF image | `PT_LOAD` segments; entry typically `0x10000` |
| `0x0000_7F00_0000_0000` | heap | `mmap` cursor, grows up |
| `0x1000_0000_0000_0000` | stack | grows down; `argc`/`argv`/`envp`/`auxv` written at load |
| `0x1500_0000_0000_0000` | randomness | 5 MiB committed blob for `getrandom(2)` |
| `0x2000_0000_0000_0000` | staged files | one 64 GiB slot per file, in staging order |

The whole 2^64 space is merkleized, but only pages that are actually touched are
allocated. A glibc hello-world touches ~240 pages (960 KiB).

Two quirks worth knowing:

- **`brk(0)` always reports a 1 GiB break** regardless of actual use. glibc only calls it
  to discover the break, and never grows it here — allocation goes through `mmap`.
- **`mmap` ignores the requested length and reserves 64 GiB.** Pages are lazily allocated,
  so reserving generously costs nothing and avoids ever having to fail an allocation.
  `mremap` refuses anything at or above 64 GiB.

---

## The syscall table

`a7` selects the call; arguments arrive in `a0`–`a5`. The tables describe the fast VM.

### Process lifecycle

| `a7` | Name | Behaviour |
| ---: | --- | --- |
| 93 | `exit` | Sets exit code, marks the VM exited. Single-threaded, so identical to `exit_group`. |
| 94 | `exit_group` | Same. |
| 96 | `set_tid_address` | Returns 0. |
| 178 | `gettid` | Hardcoded 0. |
| 179 | `sysinfo` | Fixed Linux uapi struct (1337 s uptime, 1 GiB total and free RAM, `mem_unit=1` at offset 104, everything else zero). Implemented in fast, slow and `RISCV.sol`; fast/slow agreement is tested. |
| 220 | `clone` | Returns 1 without creating a thread — not supported. |
| 435 | `clone3` | Returns -1 — not supported. |
| 258 | `riscv_hwprobe` | Returns -1 with `ENOSYS` (38) in `a1`, so the trace cannot depend on host CPU features. |

### Memory

| `a7` | Name | Behaviour |
| ---: | --- | --- |
| 214 | `brk` | Reports a fixed 1 GiB program break. |
| 222 | `mmap` | With `addr=0`, returns the heap cursor and advances it. With a hint, honours the hint. Always reserves 64 GiB; pages are allocated lazily. |
| 215 | `munmap` | No-op, returns success. Memory is never reclaimed. |
| 216 | `mremap` | Returns the old address unchanged. Reverts if the new size is ≥ 64 GiB. |
| 226 | `mprotect` | No-op. There is no MMU and no page protection. |
| 233 | `madvise` | No-op. |

### Files

| `a7` | Name | Behaviour |
| ---: | --- | --- |
| 56 | `openat` | Staged file → new descriptor. Paths are limited to 255 bytes. `/proc`, `/sys`, `/dev`, `/run` → `EACCES`. Anything else → **revert**. |
| 57 | `close` | Frees the descriptor. Reverts on an unknown one. |
| 62 | `lseek` | Staged descriptors only; an unknown descriptor or `whence` reverts. `SEEK_SET` sets the offset, `SEEK_CUR` adds a non-negative offset, and `SEEK_END` sets *size − offset* (not POSIX's size + offset). A result past EOF, or a negative offset, leaves the cursor unchanged and returns `a0 = 0`, `a1 = 0x22`. |
| 63 | `read` | Copies at most 32 bytes from the file's slot into the guest buffer; callers must handle legal POSIX short reads. fd 0 reads 0 bytes. An unknown descriptor returns -1 with `a1 = 0x4d`. |
| 64 | `write` | fd 1/2 → host stdout/stderr. fd 4/6 → oracle. Anything else, including staged descriptors, → -1 with `a1 = 0x4d`. |
| 25 | `fcntl` | `F_GETFD`/`F_GETFL` report fixed flags for reserved and staged descriptors; other commands return `EINVAL`. |
| 79 | `stat` | Returns 0 without filling the buffer. |
| 291 | `statx` | Returns 0 without filling the buffer. |
| 78 | `readlinkat` | Returns 0. |

### Time, signals, scheduling

| `a7` | Name | Behaviour |
| ---: | --- | --- |
| 113 | `clock_gettime` | Writes a constant 1337 s + 42 ns. |
| 123 | `sched_getaffinity` | Returns 0. |
| 124 | `sched_yield` | Returns 0. Nothing to yield to. |
| 73 | `ppoll` | Returns 0. |
| 98 | `futex` | No-op. No contention is possible. |
| 99 | `get_robust_list` | Returns 0. |
| 132 | `sigaltstack` | No-op. |
| 134 | `rt_sigaction` | No-op. Signals are never delivered. |
| 135 | `rt_sigprocmask` | No-op. |
| 163 | `getrlimit` | `RLIMIT_NOFILE` → soft and hard limit 1024. Other resources revert. |
| 261 | `prlimit64` | Returns 0. |

### Randomness

| `a7` | Name | Behaviour |
| ---: | --- | --- |
| 278 | `getrandom` | Serves up to 8 bytes per call from the committed blob. |

### Explicitly unsupported

| `a7` | Name | Why |
| ---: | --- | --- |
| 101 | `nanosleep` | There is no clock to sleep against. |
| 422 | `futex_time64` | Same reason as `futex`, but reverts rather than no-ops. |
| — | anything else | Reverts with `0xf001ca11` and the syscall number. |

Reverting on an unknown syscall is deliberate. Silently returning 0 would let a guest
proceed on a false premise and produce a wrong answer that both parties compute
identically — the one failure mode a fraud proof cannot catch.

---

## Calling convention

Results are returned as **`a0` = value, `a1` = an error code**, with `a0 = -1` on most
failures. The error codes are MLVM's own constants (for example `0x4d` for a bad
descriptor) rather than a faithful copy of Linux errno numbering, and `lseek` reports its
errors with `a0 = 0` (see the table above).

This differs from real Linux on RISC-V, which packs `-errno` into `a0` alone. It works for
C and Rust guests because glibc and `std` only test `a0` for a negative value.

Go guests boot again. The Go runtime's start-up check calls `fcntl(F_GETFD)` on fds 0–2 and
newer runtimes probe `riscv_hwprobe`; both are now answered (see the tables above). The
inherited `TestMinimal` Go guest passes in fast, slow and embedded-EVM mode. These tests are
opt-in and not part of CI, because the Go guest binaries are build artifacts:

```bash
make -C tests/go-tests bin bin/minimal
MLVM_RUN_GO=1 go test ./rvgo/test/ -run TestMinimal -v
```

---

## File descriptors

| fd | Purpose |
| ---: | --- |
| 0 | stdin — always reads 0 bytes |
| 1 | stdout — forwarded to the host |
| 2 | stderr — forwarded to the host |
| 3 | hint read (oracle) |
| 4 | hint write (oracle) |
| 5 | pre-image read (oracle) |
| 6 | pre-image write (oracle) |
| 100–1023 | staged files, handed out in `openat` order |

Descriptors start at 100 so a staged file can never collide with a reserved channel.
Descriptor numbers are not reused after `close`, so one execution can perform at most 924
staged `openat` calls.

---

## In-memory file system

Implemented in [`rvgo/fast/file.go`](../rvgo/fast/file.go). This is how a model
checkpoint gets in front of an inference program without any I/O ever leaving the VM.

At load time each `--load-file` is copied into emulated memory at the next 64 GiB-aligned
slot:

```bash
bin/mlvm load-elf --path bert --load-file model.safetensors \
                  --load-file tokenizer.json --load-file config.json
```

```
model.safetensors  -> 0x2000_0000_0000_0000
tokenizer.json     -> 0x2000_0010_0000_0000
config.json        -> 0x2000_0020_0000_0000
```

At run time `openat` looks the name up, `read` copies from the slot into the guest buffer
and advances a per-descriptor cursor, `lseek` moves it. A staged-file `read` returns at most
32 bytes per call. This keeps one transition within two source and two destination Merkle
leaves; standard C and Rust I/O routines already handle short reads.

The snapshot stores a versioned, sorted manifest. `EncodeWitness()` commits an authenticated
file tree and descriptor tree, including the allocation cursor, next descriptor, and
offsets. Passing a snapshot to `mlvm run` therefore restores its file state rather than
reconstructing it from host files.

Things to know:

- **Names are compared verbatim.** There is no path resolution, no working directory and
  no symlinks. The guest must open a file under the exact string it was staged with. This
  is why `scripts/run-elf.sh` runs everything from the repo root.
- **Names are at most 255 bytes.** The terminating NUL and pathname bytes touch at most nine
  32-byte leaves at any alignment. `openat` records those branches in generated witnesses.
- **The manifest and fd space each have 1,024 Merkle leaves.** At most 1,024 files can be
  staged. Staged descriptors occupy fd 100 through 1023; file syscalls append a fixed
  992-byte authentication proof.
- **Files are read-only.** `write` to a staged descriptor returns -1 with `a1 = 0x4d`.
- **The slots are 64 GiB regardless of file size**, so there is room for any checkpoint and
  no chance of one file's address range running into the next.

Try it:

```bash
make examples
./scripts/run-elf.sh examples/build/fileio examples/data/sample.txt
```

---

## Randomness

`getrandom(2)` cannot call the host RNG. Two parties replaying the same trace have to
observe the same bytes, or they will disagree about a state that neither of them got wrong.

So randomness is an **input to the execution, committed like any other memory**. By
default the region at `0x1500_0000_0000_0000` is sparse and zero-filled. An explicit
`--random-file` maps caller-provided deterministic bytes there instead. `getrandom` walks
a committed cursor through the region, wrapping at 5 MiB.

Each call serves at most 8 bytes; a guest asking for more gets 8 and a warning in the
trace. glibc uses `getrandom` for stack-guard cookies and malloc's hash seed, which is why
even a hello-world hits it.

To use your own entropy, generate a blob of at most 5 MiB and pass `--random-file`:

```bash
head -c 5242880 /dev/urandom > my-random
bin/mlvm load-elf --path prog --random-file my-random ...
```

The blob is part of the committed initial state, so both parties must agree on it — which
is exactly the property you want.

The description above is the fast VM. `getrandom` is **not** equivalent across the three
implementations: the slow Go interpreter has no case for syscall 278 and reverts with
`unrecognized system call`, and `RISCV.sol` returns the requested count without writing any
bytes or advancing the cursor. A `getrandom` transition therefore cannot currently be
checked by either verifier, even though the blob and cursor are committed.

---

## The pre-image oracle

Inherited from upstream asterisc and central to how op-stack fault proofs work, though not
used by MLVM's ML guests today.

The idea: the guest cannot fetch data, but it can ask for the pre-image of a hash it
already knows. It writes a key to fd 6, then reads the corresponding bytes from fd 5. The
key and offset are part of the state witness, so the on-chain verifier can check that the
data the challenger supplied really does hash to the key the guest asked for.

fd 3/4 carry *hints* — advisory messages that let a host-side server pre-fetch data. Hints
are not proven and cannot affect the result.

---

## What is and is not proven

**Covered by the state commitment**

- all of memory, including staged file contents and the randomness blob
- all 32 integer and 32 float registers
- PC, step counter, heap cursor, load reservation, exit status
- the pre-image key and offset
- the randomness base address and cursor
- an authenticated root of the staged-file manifest, allocator, descriptors, and offsets

**Not covered**

- `LastHint`, which is explicitly advisory
- host paths or host file contents after the initial snapshot has been built

For a staged-file syscall, the proof byte string ends with a fixed 31-word trailer. It
authenticates one file leaf and one fd-indexed descriptor leaf against the pre-state root.
The verifier checks path hashes, offsets and bounded memory accesses, then derives the new
descriptor root. Fast, slow, and `RISCV.sol` cover `openat`, staged `read`, `fcntl`, `lseek`,
and `close`; this does not make the surrounding full-trace dispute protocol complete.

Committed state is not the same as a checkable transition. The next section lists which
syscalls the verifiers can actually step.

---

## Coverage outside the fast VM

A syscall that the fast VM executes but a verifier does not reproduce is executable, not
settleable. Read from the `switch a7` blocks in `slow/vm.go` and `RISCV.sol`:

| Syscall numbers | fast | slow Go | `RISCV.sol` |
| --- | --- | --- | --- |
| 25, 56, 57, 62, 63, 64, 93, 94, 113, 123, 124, 132, 134, 135, 163, 178, 179, 214, 220, 222, 233, 258 | implemented | same behaviour | same behaviour |
| 101, 422 | revert | revert | revert |
| 73, 78, 96, 99, 226 | stub, returns 0 | **reverts** (unrecognized) | stub, returns 0 |
| 261 (`prlimit64`) | returns 0 | **reverts** | **reverts** |
| 278 (`getrandom`) | serves committed bytes | **reverts** (unrecognized) | **returns the count, writes nothing** |
| 79, 98, 215, 216, 291, 435 | stub or no-op | **reverts** (unrecognized) | **reverts** (unrecognized) |

"Same behaviour" means the slow and Solidity sources contain a case that mirrors the fast VM;
cross-implementation tests cover the staged-file calls (fast, slow and embedded EVM),
`sysinfo` (fast and slow), and whatever the conformance suites and the opt-in Go guest
exercise. Traced on 2026-10-16, glibc start-up in all five C examples calls 78, 79, 96, 99,
226, 261 and 278, and the BERT guest additionally calls 73, 215, 216 and 291. None of those
complete traces can therefore be replayed by the slow interpreter or `RISCV.sol` from start
to finish. The opt-in Go `TestMinimal` guest, by contrast, avoids them: its whole trace
(about 237,000 steps) replays step for step through both.

---

## Adding a syscall

Guests fail loudly on unimplemented syscalls, so the workflow is short:

**1. Find out what is missing.**

```bash
TRACE=1 ./scripts/run-elf.sh examples/build/yourprog 2>&1 | tail -30
# ...
# syscall: 131
# error: revert f001ca11: revert: unrecognized system call: 131
```

**2. Look it up.** The RISC-V Linux numbers are in the
[syscall table](https://jborza.com/post/2021-05-11-riscv-linux-syscalls/); 131 is `tgkill`.

**3. Add a case** in the `sysCall` closure in `fast/vm.go`:

```go
case 131: // tgkill - we never deliver signals, so nothing to do
    setRegister(toU64(10), toU64(0))
    setRegister(toU64(11), toU64(0))
```

**4. Ask whether it can change the answer.** A no-op is only safe if the guest cannot
observe the difference. If the syscall would return host state — a time, a PID, a device
capability — return a constant instead, and make sure that constant is part of the
committed state if the guest can accumulate it. That is the whole reason `getrandom` reads
from a blob rather than returning zeros.

**5. Mirror it.** A syscall that only exists in `fast` cannot be settled on chain. The same
case has to appear in `slow/vm.go` and `rvsol/src/RISCV.sol`.
