# Benchmarks

The throughput, witness-size and instruction-mix numbers were produced by `make bench` on the
machine listed below; the later sections name their own commands. Reproduce them with:

```bash
make bench                        # full run
./scripts/bench.sh --markdown     # the tables in this file
make determinism                  # the floating-point section
```

**Test machine**

| | |
| --- | --- |
| CPU | Apple M3 Max, 14 cores |
| Memory | 36 GiB |
| OS | Darwin 25.5.0 (macOS) |
| Go | 1.22.1 for the timings below. They predate the module's Go 1.23 requirement; the step counts, page counts and instruction mix were re-checked with go1.27.2 on 2026-10-16 and are unchanged. |
| Guest toolchain | `riscv64-unknown-linux-gnu-gcc` 13.2.0, `-O2 -march=rv64imafd -mabi=lp64d -static` |

---

## Contents

- [Emulation throughput](#emulation-throughput)
- [Proving throughput](#proving-throughput)
- [Witness size](#witness-size)
- [Instruction mix](#instruction-mix)
- [Floating-point determinism](#floating-point-determinism)
- [Real models](#real-models)
- [Where floating point should live](#where-floating-point-should-live)
- [The cost of verifiability](#the-cost-of-verifiability)
- [Conditional cost sketch for a future dispute](#conditional-cost-sketch-for-a-future-dispute)

---

## Emulation throughput

Fast mode, without proof capture. A future dispute protocol could use this execution path
for trace replay and hash queries.

| Program | Steps | Wall clock | Throughput |
| --- | ---: | ---: | ---: |
| `hello` (glibc, no float) | 218,199 | 8.7 ms | 25.2 M instr/s |
| `hello-libc` (glibc, debug info) | 2,100,066 | 83.8 ms | 25.1 M instr/s |
| `ieee` (dependent float chain) | 263,030 | 10.7 ms | 24.5 M instr/s |
| `matmul` (32×32 double GEMM) | 486,505 | 21.5 ms | 22.7 M instr/s |
| `mlp` (16-32-32-4 forward pass) | 289,482 | 11.9 ms | 24.4 M instr/s |

These whole-program samples cluster around 25 M instructions/s despite very different
instruction mixes. That shows no large workload-level collapse from software floating
point in this interpreter; it is not an isolated measurement of individual SoftFloat cost.

Against native execution the gap is far larger than this rate suggests: the transformer runs
below were 673–1,627× slower than native (see
[The cost of verifiability](#the-cost-of-verifiability)), so a workload that takes 10 seconds
natively can take hours to emulate. Whether that is
acceptable depends on the replay strategy of a future integration.

## Proving throughput

Proof capture records the memory branches for a selected step. A future challenger could
use this after an outer protocol identifies a terminal transition.

| Program | Per step | Rate | Witness per step |
| --- | ---: | ---: | ---: |
| `hello` | 8.0 µs | 126 K steps/s | 3,237 B |
| `hello-libc` | 7.0 µs | 144 K steps/s | 3,241 B |
| `ieee` | 7.5 µs | 133 K steps/s | 3,237 B |
| `matmul` | 7.5 µs | 134 K steps/s | 3,239 B |
| `mlp` | 7.5 µs | 133 K steps/s | 3,239 B |

Proof capture costs roughly two orders of magnitude more than plain emulation in this
sample. The tooling therefore keeps normal replay and selected-step proof generation as
separate modes.

Full re-merkleization of the whole address space, which is what a snapshot costs:

| Program | Working set | Full merkle root |
| --- | ---: | ---: |
| `hello` | 960 KiB (240 pages) | 31.2 µs |
| `hello-libc` | 2.6 MiB (675 pages) | 40.4 µs |
| `matmul` | 980 KiB (245 pages) | 38.4 µs |

Working sets here exclude the optional 5 MiB randomness blob, which `mlvm load-elf` can map
(adding 1,280 pages) but the benchmark harness does not.

Sub-linear in memory size, because unallocated subtrees hash to precomputed constants and
clean nodes are served from cache.

## Witness size

The raw state-and-branch payload consumed by a one-step verifier is:

```
state witness    674 bytes
  memory root      32     keccak root of the entire 2^64 address space
  preimage key     32
  preimage offset   8
  pc                8
  exit / exited     2
  step              8
  heap              8
  load reservation  8
  x0..x31         256
  f0..f31         256
  randomness       16     base address + cursor
  fcsr              8     flags + rounding mode
  file-system root 32     canonical metadata digest

memory proof    1,920 bytes    60 keccak nodes = one merkle branch
             (+ 1,920 more when the access straddles a 32-byte boundary)

total           2,594 bytes typical, 4,514 bytes for a two-branch CPU step
```

These figures cover the benchmarked CPU transitions. A bounded staged-file `read` may carry
five branches total (instruction, two source, two destination), and `openat` may carry up to
ten; both also append the fixed 992-byte file-system proof. The slow Go interpreter and
`RISCV.sol` authenticate those proofs for `openat`, staged `read`, `fcntl`, `lseek` and
`close` (read-only files, at most 1,024 staged files and fds, 32 bytes per `read`, 255-byte
names). Other syscalls, including `getrandom`, are not yet equivalent across the three
implementations; see [SYSCALLS.md](SYSCALLS.md#coverage-outside-the-fast-vm).

The 2,000-step benchmark sample averages about 3,239 bytes per step, so roughly one third of
sampled instructions require a second 32-byte branch.

The payload is independent of the whole program's working-set size: the memory root remains
32 bytes whether the guest touched one page or 100,000. ABI framing and outer-game calldata
are not included.

## Instruction mix

`mlvm run` reports this at exit, and it tells you where a workload's cost is:

| Program | Steps | Integer ALU | Load/store | Float ALU | Float ld/st | Pages |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `hello` | 218,199 | 55.4% | 18.9% | 0.0% | 0.0% | 240 |
| `hello-libc` | 2,100,066 | 55.6% | 18.7% | 0.0% | 0.0% | 676 |
| `ieee` | 263,030 | 52.3% | 17.4% | 6.9% | 0.0% | 240 |
| `matmul` | 486,505 | 42.1% | 9.2% | 15.0% | 14.3% | 246 |
| `mlp` | 289,482 | 53.2% | 17.3% | 3.9% | 2.2% | 246 |

Three observations:

**Process start-up dominates small programs.** A glibc "hello world" is ~218,000
instructions, essentially all of it before `main`: TLS setup, auxv parsing, stdio buffers,
locale init, malloc arena setup. The `printf` itself is noise.

**`matmul` is the only genuinely float-bound workload here**, at 29% of instructions in the
FPU — and half of those are loads and stores rather than arithmetic, because a 32×32 GEMM
streams far more matrix elements than it multiplies at any instant. That is the profile a
real inference workload has, and it is where the SoftFloat cost would show up if it were
significant.

**`ieee` is arithmetic with almost no float memory traffic** (6.9% FPU, 0.0% float
load/store): its whole working set lives in registers across the dependent chain. It is the
cleanest measurement of raw SoftFloat cost in the set, and it runs at full speed.

The counters cover the opcode classes the profiler tracks; branches, jumps and system
instructions execute but are not counted, which is why the percentages do not sum to 100.

## Floating-point determinism

The property the whole design rests on: two honest parties on different hardware must
compute the same bits. `make determinism` compiles identical C twice — once for RV64IMAFD,
once for the host — runs one in the VM and one natively, and diffs.

### Pure IEEE-754 arithmetic: bit-exact

`examples/ieee.c` runs about 16,000 dependent operations with no exactly-representable
operands, plus gradual underflow into the subnormal range:

```
=== ieee ===============================================
IDENTICAL - MLVM softfloat == host FPU, byte for byte
    sum       = 3.8956542839085366
    product   = -0.82400427912558161
    sqrt(sum) = 1.9737411896975086
    subnormal = 5.5626846462679985e-309
    underflow = 2.1849137534174492e-08
```

`examples/matmul.c` — a 32×32 double-precision GEMM — likewise matches exactly.

**Software IEEE-754 on RISC-V agrees with an ARM hardware FPU to the last bit**, through
addition, subtraction, multiplication, division, square root and gradual underflow.

### Why this test is not enough on its own

`make determinism` runs one dependent chain of operations. That is a strong integration
signal, but it samples a single trajectory through the input space — and for a long time it
passed while `F64_div` was returning a quotient one unit in the last place off on about 2% of
inputs, because the chain never landed on one of them. Five transformer models did not catch
it either: a wrong-but-deterministic double is indistinguishable from a right one.

`TestSoftFloatAgainstNativeFPU` is the complement. It compares `rvgo/comp` against the host
FPU on **200,000 random operand pairs per operation**, drawn deliberately toward subnormals,
wide exponent gaps and exact ties, and skips pairs whose correct result is NaN (which leaves
160,049 for `√`). It exercises only round-to-nearest-even, the SoftFloat package default and
the host's; other rounding modes are covered by the conformance suites. For `+ − × ÷ √` the
correctly-rounded result is unique, so any disagreement is a bug in one of them rather than
a matter of interpretation:

```console
$ go test ./rvgo/test/ -run TestSoftFloatAgainstNativeFPU -v
    F64_add   200000 operand pairs, bit-identical to the host FPU
    F64_sub   200000 operand pairs, bit-identical to the host FPU
    F64_mul   200000 operand pairs, bit-identical to the host FPU
    F64_div   200000 operand pairs, bit-identical to the host FPU
    F64_sqrt  160049 operand pairs, bit-identical to the host FPU
```

It found two bugs on its first run, both in code that had shipped.

### Where guest and host still diverge

> **Note.** These figures were first measured while `FMADD` was emulated unfused, which was
> one cause. It is genuinely fused now, and the experiment settled the question:
> `examples/mlp.c` improved from ~13 units in the last place to **1**, while each transformer's
> post-fix MLVM score was bit-identical to its own pre-fix MLVM score. This does not mean the
> MLVM and host scores are identical. See
> [What fusing the multiply-add actually changed](#what-fusing-the-multiply-add-actually-changed).

`examples/mlp.c` diverges in the last 2–3 digits:

```
host   logit[0] =  0.68384752984497377
mlvm   logit[0] =  0.68384752984497388
```

Two sources have mattered during the project's history:

**1. FMADD was not fused** — `add(mul(a,b), c)`, two roundings where hardware does one.
**Fixed:** `Softfloat_mulAddF64` had been ported all along and was simply never called. This
turned out to matter for C guests and not at all for the Rust ones — see below.

**2. libm is not the ISA.** This is the remaining source. `tanh()` and `exp()` are library
routines, not instructions.
glibc's implementations differ from Apple's and musl's in the last ULP, and no emulator can
reconcile that — the two programs are running different code. Rebuilding both sides with
`-ffp-contract=off` removes cause 1 and the divergence persists, which isolates cause 2.

The practical rule for anyone putting inference on chain: **the ISA is deterministic, the C
library is not.** Ship the transcendental implementations with the guest, or restrict
yourself to arithmetic the ISA defines. `determinism.sh` reports this case separately, as
an expected divergence rather than a failure.

## Real models

Five transformer encoders, run inside the VM by a
[Candle](https://github.com/huggingface/candle)-based guest,
[`examples/rust/bert-riscv`](../examples/rust/bert-riscv). The guest is not a stock pipeline:
it changes tokenizer batching, checkpoint key names and pooling (see
[Limitations of the model measurements](#limitations-of-the-model-measurements)).
Reproduce with:

```bash
make models        # downloads ~1.2 GB of checkpoints, then runs each one
make charts        # regenerates the figures below from results/model-bench.tsv
```

| Model | Params | Layers | Hidden | Steps | Float % | Memory | Wall clock | Cosine score |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `all-MiniLM-L6-v2` | 22.7M | 6 | 384 | 925M | 57.7% | 285 MiB | 47 s | `0.6110916734` |
| `bge-small-en-v1.5` | 33.4M | 12 | 384 | 1,697M | 63.0% | 408 MiB | 87 s | `0.7658209205` |
| `all-MiniLM-L12-v2` | 33.4M | 12 | 384 | 1,690M | 63.1% | 407 MiB | 87 s | `0.6257025003` |
| `bge-base-en-v1.5` | 109.5M | 12 | 768 | 6,061M | 69.0% | 1,280 MiB | 309 s | `0.7760061622` |
| `bert-base-uncased` | 110.1M | 12 | 768 | 6,107M | 68.5% | 1,288 MiB | 309 s | `0.6334546804` |

The cosine score is the guest's own: mean pooling over all eight token positions, padding
included, with no attention mask. It is not the score the standard `sentence-transformers`
pipeline reports for these checkpoints. Every row is a single run recorded with an earlier
VM build; the current build serves staged-file reads at most 32 bytes at a time, and
`all-MiniLM-L6-v2` now takes 1,062,486,264 steps (re-run 2026-10-16, same score).

Those runs demonstrate fast-VM scale only. MLVM can generate a single-step witness for
selected supported transitions, but these traces are not settleable by the experimental
contracts: their arithmetic is `f32`, which has no nested settlement route, and they call
syscalls the slow interpreter and `RISCV.sol` do not reproduce. When these rows were
recorded the guest only printed its score; it now also commits it at `MLVM_OUT`, in the
layout of `mlvm_result.h`, so the score is bound to the post-state. See
[Project status](STATUS.md).

![instruction mix per model](fig/model-instruction-mix.svg)

**These are genuinely floating-point-bound workloads.** Between 58% and 69% of the executed
trace is float ALU or float load/store — and the share *rises* with model size, because a
larger hidden dimension means more arithmetic per token of tokenizer and allocator
overhead. This is the regime that fault-proof VMs have historically declined to enter.

![steps and memory per model](fig/model-scaling.svg)

Cost tracks the model, not the machine: 4.8× the parameters costs 6.6× the instructions,
which is what you would expect when attention is quadratic in nothing here (sequence length
is fixed at 8 tokens) but the feed-forward blocks scale with hidden².

![throughput per model](fig/model-throughput.svg)

**Throughput barely moves.** `bge-base` executes 4.2 billion floating-point instructions
through software IEEE-754 and still runs at 19.6 M instr/s, against ~25 M for a workload
with no floating point at all. The software FPU costs roughly 20% — and part of even that
is the 1.3 GiB working set, not the arithmetic.

### Where the instructions go

A failed load of `bert-base-uncased` — stopped after parsing the checkpoint but before the
forward pass — isolates the two phases:

| Phase | Steps | Share |
| --- | ---: | ---: |
| Parse and materialise a 420 MiB safetensors checkpoint | ~391M | 6.4% |
| Tokenise and run the forward pass | ~5,716M | 93.6% |

Model loading was not the bottleneck in that measurement, even at 420 MiB, because the
checkpoint is already in the emulated address space — `read` is a memory copy, not I/O.
The split was taken with the earlier VM build. The current 32-byte cap per staged-file
`read` makes loading more expensive: one `read` call per 32 bytes of checkpoint.

### Determinism on real models

The same source, compiled twice: once for RV64IMAFD, once for the host. This is a
cross-build portability comparison, not an isolation of the VM's arithmetic.
`examples/rust/bert-host` and `examples/rust/bert-riscv` share a byte-identical
`src/main.rs` and the same locked versions of Candle and `tokenizers` (their `Cargo.lock`
files differ only in `bumpalo`). The two runs still differ in more than the machine: one is
an arm64 macOS build using the host FPU, Apple's libm and the host `std`; the other is a
`riscv64imafd-unknown-linux-gnu` build with its own `std`, statically linked glibc libm and
code generation, whose `f32` arithmetic MLVM executes in software.

```bash
make model-determinism        # results/model-determinism.tsv
```

| Model | Steps | MLVM | Host (arm64) | Distance |
| --- | ---: | ---: | ---: | ---: |
| `all-MiniLM-L6-v2` | 925M | `0.6110916734` | `0.6110918522` | 3 ULP |
| `bge-small-en-v1.5` | 1,697M | `0.7658209205` | `0.7658217549` | 14 ULP |
| `all-MiniLM-L12-v2` | 1,690M | `0.6257025003` | `0.6257022619` | 4 ULP |
| `bge-base-en-v1.5` | 6,061M | `0.7760061622` | `0.7760072947` | 19 ULP |
| `bert-base-uncased` | 6,107M | `0.6334546804` | `0.6334548593` | 3 ULP |

### What fusing the multiply-add actually changed

The two causes above were a hypothesis. Fixing one of them tested it, and the answer was not
what the hypothesis predicted.

`FMADD` is now genuinely fused — one rounding over `a*b + c` rather than two. Re-running the
whole determinism sweep afterwards:

| Guest | Before fusing | After fusing |
| --- | ---: | ---: |
| `examples/mlp.c` (C, glibc) | ~13 ULP | **1 ULP** |
| `all-MiniLM-L6-v2` | 3 ULP | 3 ULP |
| `bge-small-en-v1.5` | 14 ULP | 14 ULP |
| `all-MiniLM-L12-v2` | 4 ULP | 4 ULP |
| `bge-base-en-v1.5` | 19 ULP | 19 ULP |
| `bert-base-uncased` | 3 ULP | 3 ULP |

For each transformer, its post-fix MLVM score was **bit-identical to its pre-fix MLVM
score**. Fusing changed nothing on the executed paths in those guest binaries. The 3–19 ULP
table above still measures the separate MLVM-versus-host comparison.

The reason is *where the multiply-adds come from*. C compiled against glibc gets them from
libm's polynomial evaluation of `tanh` and `exp`, so fusing them moves the answer. Rust does
not contract `a*b + c` into an FMA unless asked to, so candle's kernels never emitted one and
both sides of that comparison already agreed — the guest ELF contains 61 multiply-add
instructions, but none on a path that reaches the score.

So for the models, cause 1 was never operating: **the 3–19 ULP residue comes from what
differs between the two builds** — chiefly two different implementations of transcendental
functions such as `erf`, `tanh` and `exp`, alongside different targets' `std` and code
generation. It is not something a more careful instruction set could fix, and reproducible
inference needs a pinned math library and build, not just a deterministic ISA. The
comparison does not separate those contributions from each other.

Distance is measured in units-in-the-last-place of the float32 the model actually produces.
**A 6.1-billion-instruction transformer inference lands within 3 ULP of native hardware**,
and the worst case across the five is 19 — roughly one part in 10^6.

Note that error does not grow with trace length: `bert-base-uncased` executes 6.6× the
instructions of `all-MiniLM-L6-v2` and lands at the same 3 ULP, while `bge-base-en-v1.5`
at a comparable length is the worst of the set. The divergence is a property of the
arithmetic each model happens to perform, not of accumulated drift.

That is a strong result and a limitation at the same time. It is strong because the two
runs share no arithmetic implementation whatsoever, and accumulate across billions of
operations without drifting materially. It is a limitation because a fraud proof needs
*exact* agreement, and this is not exact.

The distinction that resolves it: a dispute is settled between two parties who both run
**the VM**, and the VM agrees with itself bit-for-bit — the same input produces the same
674-byte witness every time. Matching *native hardware* is a strictly stronger property,
useful for validating a port and for reproducibility. The historical FMADD defect is fixed;
the remaining cross-build gap comes from transcendental implementations outside the ISA, as
described [above](#where-guest-and-host-still-diverge).

### Limitations of the model measurements

Read the model tables with these limits in mind:

- **The guest is modified Candle code, not a reference pipeline.** It tokenises sentences one
  at a time and pads by hand, renames `bert.`-prefixed and `gamma`/`beta` checkpoint keys,
  and mean-pools over all token positions, padding included, with no attention mask. Scores
  are comparable between MLVM and `bert-host`, not with published `sentence-transformers`
  results.
- **Inputs are not pinned.** `scripts/model-bench.sh --fetch` downloads `config.json`,
  `tokenizer.json` and `model.safetensors` from each Hugging Face repository's mutable `main`
  revision, and no file — tokenizers included — is checked against a hash. A later fetch
  can produce different bytes and different scores.
- **Each row is one run.** `results/model-bench.tsv` and `results/model-determinism.tsv` hold
  a single run per model, with no repetitions or variance. The raw run logs are not committed
  (`results/*.log` is gitignored), and the native timings in
  [The cost of verifiability](#the-cost-of-verifiability) are not recorded in `results/` at
  all.
- **The VM build has moved on.** The rows were recorded before staged-file reads were capped
  at 32 bytes, so current step counts and wall clocks are higher (see above).
- **The answer was not committed.** The guest that recorded these rows printed its score
  without writing it through `MLVM_OUT`, so no score in these tables is bound to a
  post-state. The current guest commits it (2,202 more instructions on all-MiniLM-L6-v2).

### Porting notes

Two things blocked real Rust ML guests, and both are worth knowing before you port one:

**Parallel iterators.** `tokenizers::encode_batch` parallelises with rayon, which builds a
global thread pool on first use. MLVM refuses `clone(2)` — deliberately, since concurrent
execution has no deterministic interleaving to prove — so the pool cannot be built and the
call panics with `ThreadPoolBuildError { IOError(PermissionDenied) }`. Encoding
sequentially produces identical token IDs. This is the single most common obstacle: the
framework is fine, a parallel iterator in the data path is not.

**Checkpoint naming.** Task-head checkpoints store the encoder under a `bert.` prefix, and
the original 2018 BERT release names LayerNorm parameters `gamma`/`beta` rather than
`weight`/`bias`. The guest normalises both, which is what lets `bert-base-uncased` load
alongside modern sentence-transformers checkpoints.

## Where floating point should live

Every verifiable virtual machine faces one design choice about floating point: implement it
in the interpreter, or omit it and let the compiler expand each operation into integer
soft-float helpers. Most fault-proof and zero-knowledge VMs take the second option. This
measures what that costs.

```bash
./scripts/float-placement.sh
```

[`examples/rust/float-placement`](../examples/rust/float-placement) builds one `no_std`
kernel — a repeated 24×24 double-precision GEMM — for both targets and runs both in the
same VM. `no_std` deliberately: no libc, no allocator, no libm, so the difference is
attributable to floating point and nothing else.

| Build | Float instructions in `.text` | Steps |
| --- | ---: | ---: |
| `rv64imafd` — VM executes `FADD.D` via SoftFloat | 66 | **903,446** |
| `rv64ima` — compiler expands into integer code | 0 | **22,898,400** |

**25.3× more steps when the compiler does it.** Broken down, each double-precision
arithmetic operation expands into about **91 integer instructions** — which is what
`__adddf3` and `__muldf3` look like, with their branchy exponent alignment, normalisation
and rounding paths.

And the two builds agree exactly:

```
trace     = 0xbfe61436da4b68e0
frobenius = 0x40551af81e2d3f22
C[0][0]   = 0x3fac51aec2906e04
```

Bit-identical, from two entirely independent IEEE-754 implementations — MLVM's SoftFloat
port and Rust's `compiler_builtins`.

**Why this matters beyond MLVM.** Step count is not an incidental metric. It sets replay
cost during bisection, and for a zero-knowledge VM it sets proving cost *linearly*. A zkVM
that omits F/D to keep its instruction set small is paying 25× on any floating-point
workload to save perhaps a few hundred lines of interpreter. On the evidence here, that is
the wrong trade: interpreter-level floating point costs about 20% of emulation throughput
and saves an order of magnitude in cycles.

## The cost of verifiability

Emulation is not free, and the honest number is large.

| Model | Native | In MLVM | Slowdown |
| --- | ---: | ---: | ---: |
| `all-MiniLM-L6-v2` | 0.07 s | 47.1 s | 673× |
| `bge-small-en-v1.5` | 0.09 s | 86.9 s | 966× |
| `all-MiniLM-L12-v2` | 0.09 s | 86.6 s | 962× |
| `bge-base-en-v1.5` | 0.19 s | 308.7 s | 1,625× |
| `bert-base-uncased` | 0.19 s | 309.2 s | 1,627× |

Native times are the fastest of three runs of `examples/rust/bert-host` — the same source
and locked Candle version, built for the host, on the same machine. They are not recorded
in `results/`.

**Roughly three orders of magnitude.** Two factors compound:

- **Interpretation.** MLVM retires ~20 M instructions per second; the host core retires
  billions.
- **Vectorisation.** Native candle dispatches into SIMD kernels where one instruction does
  several single-precision operations (four per 128-bit NEON register on this arm64 host).
  The guest executes scalar RISC-V, and MLVM has no vector extension.

This is the price of deterministic scalar interpretation. A future optimistic protocol
could keep it off the normal serving path and use it for reproducible replay, but MLVM does
not currently provide that protocol, its trace commitments, or a challenger service. These
numbers therefore describe an execution substrate, not production dispute latency.

It also says where the performance work is. The vector extension would close much of the
gap and is the single most valuable extension to add — see [ISA.md](ISA.md#not-implemented-by-design).

## Conditional cost sketch for a future dispute

The following is an illustrative decomposition for a trace of about 2×10^9 steps — between
the recorded `bge-small` and `bge-base` runs, whose native equivalents take well under a
second. It is not a benchmark of an implemented MLVM game:

| Stage | Cost |
| --- | --- |
| Emulate the full trace | ~2×10^9 steps ≈ 80 s at 25 M instr/s |
| Locally query hashes to isolate one step | ~31 rounds, potentially minutes off chain |
| Prove the disputed step | 7 µs, one time |
| Post to chain | ~2.5 KB calldata + one `RISCV.step()` call |
| Chain-side compute | **one integer instruction, ~84 K gas** (186 K worst case) |

The last row assumes the isolated step is one `RISCV.sol` executes. For the model traces
above it often would not be: in an `all-MiniLM-L6-v2` run on 2026-10-16, 343M of the 353M
float-ALU instructions were single precision (the `f64` remainder includes Candle's GELU,
which evaluates `erf` in double precision). `RISCV.sol` reverts on float arithmetic and no
nested route settles `f32` yet.

A protocol built around this witness format could keep bulk model data off chain and submit
one state witness plus the relevant branch at its terminal step. MLVM does not yet bind that
step to an on-chain trace game. Staged-file metadata is committed and its bounded syscalls
are authenticated, but `getrandom` and several stub syscalls still differ between the
implementations.

Measured on chain: all 84 integer, multiply and atomic conformance suites verify
step-for-step through `RISCV.sol` in a real EVM, averaging **83,998 gas per proven step** in
the original measurement, with a worst case of **186,089** when re-run on 2026-10-16
(`MLVM_RUN_EVM=1 go test ./rvgo/test/ -run TestEVMStep -v`, which prints per-suite
maxima but not the average).

Those EVM measurements cover supported integer transitions, not whole programs. The current
`f64` terminal experiment adds nested component measurements, while `f32`, outer-game
binding, syscall equivalence, and a deployed protocol remain open. See
[STATUS.md](STATUS.md) and [ROADMAP.md](ROADMAP.md).

---

## Reproducing

```bash
make doctor        # confirm the toolchain
make examples      # cross-compile the guests
make bench         # everything in this file except the BERT row
make determinism   # the floating-point section
```

Tune the sample count with `FAST_ITERS` and `PROOF_ITERS`:

```bash
FAST_ITERS=20 PROOF_ITERS=20000 make bench
```
