# Examples

Guided walkthroughs, in the order they build on each other. Each one is a real program in
[`examples/`](../examples) that you can run, modify and rebuild.

```bash
make doctor      # confirm you have Go and a RISC-V cross toolchain
make examples    # cross-compile everything to RV64IMAFD
make tour        # hello, mlp, determinism and the witness pipeline in one go
make demo        # the one-command TinyStories demo, with a receipt and --verify
```

---

## Contents

1. [Hello world — the syscall surface](#1-hello-world--the-syscall-surface)
2. [IEEE — deterministic arithmetic regression](#2-ieee--deterministic-arithmetic-regression)
3. [Matmul — floats at scale](#3-matmul--floats-at-scale)
4. [MLP — a neural network](#4-mlp--a-neural-network)
5. [File I/O — how a model gets in](#5-file-io--how-a-model-gets-in)
6. [The single-step witness pipeline](#6-the-single-step-witness-pipeline)
7. [Running your own program](#7-running-your-own-program)
8. [BERT — the full ML workload](#8-bert--the-full-ml-workload)

---

## 1. Hello world — the syscall surface

```bash
make hello
```

```
==> loading hello into a fresh VM state
    program entry: 10534
==> running (guest output below)
-------------------------------------------------
Hello from inside the MLVM RISC-V virtual machine!
Every instruction is visible in a deterministic RISC-V trace.

=== instruction profile =========================
  steps                  218199
  integer ALU            120890 ( 55.4%)
  memory load/store       41271 ( 18.9%)
  float ALU                   0 (  0.0%)
  float load/store           12 (  0.0%)
  pages touched            1521  (5.9 MiB)
  wall clock               10ms
  throughput              22.56 Minstr/s
=================================================
```

**218,000 instructions to print two lines.** Almost none of that is `printf`. It is glibc
starting up: thread-local storage, ELF auxv parsing, stdio buffer allocation, locale
initialisation, malloc arena setup, and the syscalls behind all of it.

That is a useful compatibility test. It demonstrates enough Linux-style process behavior
for this static glibc binary, but it does not imply general Linux, libc, C, or Rust
compatibility. Programs must still stay inside the documented ISA and syscall surface.

Watch the syscall stream:

```bash
TRACE=1 ./scripts/run-elf.sh examples/build/hello 2>&1 | grep '^syscall:'
```

The fifteen calls, annotated:

```
syscall: 214 (x2)         brk(0)          -> discover the program break
syscall: 222              mmap            -> malloc's first arena
syscall: 96               set_tid_address
syscall: 99               get_robust_list
syscall: 261              prlimit64
syscall: 78               readlinkat
syscall: 278              getrandom       -> stack-guard cookie, from the committed blob
syscall: 214 (x2)         brk
syscall: 222              mmap
syscall: 226              mprotect
syscall: 79               stat
syscall: 64               write(1, ...)   -> the actual output
syscall: 94               exit_group
```

Every one of those is a case in `sysCall()` in
[`rvgo/fast/vm.go`](../rvgo/fast/vm.go). See [SYSCALLS.md](SYSCALLS.md) for the
full table — including which of them the slow interpreter and `RISCV.sol` do not reproduce
(78, 79, 96, 99, 226, 261 and 278 all differ in at least one of them).

---

## 2. IEEE — deterministic arithmetic regression

The load-bearing claim of the whole project: two honest parties on different hardware
compute the same bits.

```bash
make determinism
```

```
=== ieee ===============================================
IDENTICAL - MLVM softfloat == host FPU, byte for byte
    sum       = 3.8956542839085366
    product   = -0.82400427912558161
    sqrt(sum) = 1.9737411896975086
    subnormal = 5.5626846462679985e-309
    underflow = 2.1849137534174492e-08
```

[`examples/ieee.c`](../examples/ieee.c) runs about 16,000 dependent operations on values that
are never exactly representable, so every one has to round. It also walks the parts of IEEE
754 that implementations usually get wrong: repeated doubling of the smallest subnormal
(`5e-324`), and a shrinking product driven down into the subnormal range.

Identical output, on an ARM Mac, from a RISC-V binary whose every `FADD.D` went through
software.

**Why this works.** MLVM never touches the host FPU. [`rvgo/comp`](../rvgo/comp)
is a Go port of Berkeley SoftFloat 3 — 121 files, ~5,800 lines — that rounds in whatever
mode each instruction selects and uses a fixed NaN policy. `ieee.c` never changes `frm`, so
every operation here rounds to nearest, ties to even. Two machines agree because neither
one's FPU is involved.

**Try to break it.** Add an operation to `ieee.c`, `make determinism ieee`, and see whether
it still matches. If you find a divergence in `+ - * / sqrt`, that is a genuine bug worth
reporting.

---

## 3. Matmul — floats at scale

```bash
./scripts/run-elf.sh examples/build/matmul
```

```
trace     = 3842.25
frobenius = 3840.819535349845
C[0][0]   = 125.25
C[31][31] = 120.5

  steps                  486505
  integer ALU            204787 ( 42.1%)
  memory load/store       44904 (  9.2%)
  float ALU               72767 ( 15.0%)
  float load/store        69692 ( 14.3%)
  throughput              24.74 Minstr/s
```

A 32×32 double-precision GEMM: 142,459 floating-point instructions, 29% of the trace. Its
whole-program throughput is close to the zero-float `hello-libc` sample, showing that
software floating point does not collapse interpreter throughput here. This comparison does
not isolate the cost of an individual operation.

Also note the split: half the float instructions are loads and stores. A GEMM streams
matrix elements far faster than it multiplies them.

---

## 4. MLP — a neural network

```bash
make mlp
```

```
MLP 16-32-32-4, tanh hidden, softmax head
logit[0] =  0.68384752984497388   p[0] = 0.46250643473837472
logit[1] = -0.22135397784448127   p[1] = 0.18706552604122356
logit[2] = -1.2616037208588595   p[2] = 0.066102675269336789
logit[3] =  0.19730629016411491   p[3] = 0.28432536395106495
argmax    = 0
checksum  = 67.660363130047585
```

A 16→32→32→4 network with `tanh` hidden layers and a softmax head, weights generated from a
fixed LCG seed so the program has no inputs at all. Its output is a pure function of ISA
semantics — which is exactly what makes it disputable bit by bit.

[`examples/mlp.c`](../examples/mlp.c) exercises what real inference exercises: the
multiply/add-heavy inner GEMM loop, `exp` and `tanh` from libm (which decompose into
multiply/add/divide plus integer exponent surgery), and `FDIV.D` for softmax
normalisation.

### The instructive part: it does *not* match natively

```bash
./scripts/determinism.sh mlp
```

```
DIVERGED (expected: this example calls libm transcendentals)
    -logit[0] =  0.68384752984497377     <- host
    +logit[0] =  0.68384752984497388     <- mlvm
```

There were two independent causes. The first was a VM arithmetic bug and is now fixed; the
second is an expected difference between separately compiled guest and host libraries:

1. **FMADD was not fused** — MLVM computed `FMADD.D` as `add(mul(a,b), c)`, two roundings
   where hardware does one. The examples build with `-ffp-contract=off`, so `mlp.c`'s own
   dot products contain no FMADD; the fused multiply-adds in this binary are in glibc's
   `exp` and `expm1` (which `tanh` uses), and those hit the bug. **Fixed:** it is genuinely
   fused now, which took this example from ~13 units in the last place to **1**.
2. **libm is not the ISA.** `tanh` and `exp` are library routines. glibc's differ from
   Apple's in the last ULP, and no emulator can reconcile that — the two binaries are
   running different code. This is what remains.

`examples/Makefile` still builds with `-ffp-contract=off`, which now only removes a source
of *compiler* variation rather than working around a VM gap.

**The practical rule for on-chain inference: the ISA is deterministic, the C library is
not.** Ship your transcendentals with the guest, or stay inside arithmetic the ISA defines.

---

## 5. File I/O — how a model gets in

```bash
./scripts/run-elf.sh examples/build/fileio examples/data/sample.txt
```

```
line 1: MLVM stages this file into emulated memory before the program starts.
line 2: The guest opens it with openat(2) and reads it with read(2), but no host
line 3: syscall is ever issued: the bytes are already inside the merkleized
---
path     = examples/data/sample.txt
lines    = 6
bytes    = 398
checksum = 14600803813687119950
```

The guest called `fopen`/`fgets`/`fclose` and got real data. No host I/O happened.

The file was copied into emulated memory at load time, at a 64 GiB-aligned slot starting
from `0x2000_0000_0000_0000`, and `openat`/`read`/`lseek` were served out of that. This is
how a 400 MiB safetensors checkpoint reaches an inference program that is, by construction,
unable to touch the outside world — and it means the model weights are covered by the same
memory root as everything else.

Two constraints to remember:

- **Names are compared verbatim.** No path resolution, no working directory, no symlinks.
  The guest must open the file under exactly the string it was staged with.
- **Forgetting to stage is a hard error**, not a silent `ENOENT`:

```
revert f001ca11: file "model.safetensors" was not staged into the VM;
pass --load-file model.safetensors
```

---

## 6. The single-step witness pipeline

```bash
make proof
```

This demonstration runs a program, selects a candidate step, writes representative replay
checkpoints, and emits the state witness and memory branch needed by a supported
single-step verifier:

```
 STEP 4  Prove the single disputed instruction
-------------------------------------------------------------------
  disputed step     109,099
  pre-state  hash   0x03f2090714ff95e0691bc6697ce5d6ff294f7d57798e62e1137113a69937cb7b
  post-state hash   0x03f3470f03de515d2c065a969ee7e50bcdf595056fc0faa014e3fb46897c11fa

  state witness     674 bytes
    memory root       639a71397cbc66e0918b7e6836480acbbed44258e7c5ac31441b1b6a5f7d0d5f
    pc                0x627d4
    step              109,099
    32 x integer reg  256 bytes
    32 x float reg    256 bytes

  memory proof      1920 bytes = 60 merkle nodes
```

**2,594 bytes.** That is the raw witness-plus-branch payload for this one-branch instruction,
independent of the program's 6 MiB working set. It excludes ABI framing and any outer-game
transactions or bookkeeping.

Then the commitment-sensitivity check, which prints recomputed hashes only when
`pycryptodome` or `pysha3` is installed:

```
 STEP 5  Commitment sensitivity: one changed bit moves the hash
-------------------------------------------------------------------
  keccak(state)             0x58f2090714ff95e0691bc6697ce5d6ff294f7d57798e62e1137113a69937cb7b
  keccak(state, a0 ^= 1)    0xb04d29974dba72caf4f184229675dfe385998bda7b3abfe16778c377da04e23d
  bits changed in the hash  114 of 256
```

(`keccak(state)` matches the pre-state hash above except for its first byte, which the VM
overwrites with a status code.)

One bit of one register changes roughly half the hash bits. This binds a checkpoint to its
state; an outer protocol is still required to bind checkpoints to a trace and adjudicate a
claim.

Artifacts are kept in `.mlvm-proof/` so you can inspect the JSON.

**Boundary:** this script generates and inspects a local single-step witness; it does not
submit it to an outer game. `RISCV.sol` can check supported integer transitions in the
current 674-byte state layout, but it does not directly execute F/D arithmetic, and syscall
coverage still differs across the three VMs. See [Project status](STATUS.md).

---

## 7. Running your own program

Any C or C++ program that compiles to RV64IMAFD and stays inside the supported syscalls
will run.

```c
// mine.c
#include <stdio.h>
int main(void) { printf("%d\n", 6 * 7); return 0; }
```

```bash
riscv64-unknown-linux-gnu-gcc -O2 -static \
    -march=rv64imafd -mabi=lp64d -ffp-contract=off \
    -o mine mine.c -lm

./scripts/run-elf.sh mine
```

Or just drop the `.c` file into `examples/` — the Makefile globs `*.c`, so `make examples`
picks it up with the right flags.

### The three flags that matter

| Flag | Why |
| --- | --- |
| `-march=rv64imafd` | **No `c`.** The compressed extension emits 2-byte instructions that this VM does not decode; an RVC binary reverts at step 0 with `unknown instruction opcode: 1`. |
| `-static` | There is no dynamic loader inside the VM. |
| `-ffp-contract=off` | Keeps native and RISC-V comparison builds from making different contraction choices. MLVM's FMADD implementation is fused. |

### When it fails

**`unknown instruction opcode: 1`** — RVC. Rebuild without `c` in `-march`.

**`unrecognized system call: N`** — you used a syscall MLVM does not implement. Run with
`TRACE=1` to see the stream leading up to it, then add a case following
[SYSCALLS.md § Adding a syscall](SYSCALLS.md#adding-a-syscall).

**`file "x" was not staged`** — pass it: `./scripts/run-elf.sh mine x`.

**Program runs forever** — check the step count with `INFO_AT='%1000000'`. Some libc paths
(locale loading, `iconv`) are enormous; a static build with `setlocale` avoided is usually
much shorter.

---

## 8. BERT — the full ML workload

The largest workload, in [`examples/rust/bert-riscv/`](../examples/rust/bert-riscv): a
Candle-based Rust guest that runs BERT-family sentence encoders (22.7M to 110M parameters)
for sentence similarity, entirely inside the fast VM. This is a workload demonstration, not
a verified inference (see [Project status](STATUS.md)).

For `all-MiniLM-L6-v2`, the smallest checkpoint, from `results/model-bench.tsv` and
`results/model-determinism.tsv`:

| | |
| --- | --- |
| Input | *"The cat sits outside"* vs *"The cat plays in the garden"* |
| Native (macOS arm64, `bert-host`) | `0.6110918522` |
| MLVM | `0.6110916734` |
| Steps | 925M (one recorded run) |
| Wall clock in the VM | 47.1 s for the sentence pair |

The two scores differ by 3 float32 ULP. The native build differs from the guest in more
than the machine: target `std`, libm and code generation differ too, and Candle's
activations go through transcendental functions outside the ISA. See
[BENCHMARKS.md](BENCHMARKS.md#determinism-on-real-models).

What had to work at once for this to run: Rust `std` on the fast VM's syscall surface, a
real tokenizer, a safetensors checkpoint (90 MB here, 440 MB for `bert-base-uncased`) read
through the in-memory filesystem, and the model's arithmetic executed in software. Candle's
`BertModel` computes in `f32`, so the hot loop is single-precision (`FADD.S`, `FMUL.S`), not
`FADD.D`: in a 2026-10-16 run, 343M of the 353M float-ALU instructions were single
precision. The experimental `f64` sub-game does not cover them.

Three caveats about what the score means:

- **It is not a stock pipeline.** The guest encodes sentences one at a time and pads them by
  hand (rayon's `encode_batch` cannot start a thread pool here), renames legacy checkpoint
  keys, and mean-pools over every token *including padding* with no attention mask. The
  score is therefore not the reference `sentence-transformers` similarity.
- **It is printed and committed.** Like `examples/mlp.c` and the TinyStories decoder, the
  guest writes its answer (`score: 0.6110916734`) to `MLVM_OUT` in the layout of
  `mlvm_result.h`, so the post-state binds it and a forged score can be disputed (see
  [GALLERY.md](GALLERY.md)). Builds before 2026-10-20 only printed it.
- **The recorded run used an earlier VM build.** The current build serves staged-file reads
  at most 32 bytes at a time (2.85 million `read` calls for this checkpoint). Re-run on
  2026-10-16 with the same guest ELF, the same checkpoint took 1,062,486,264 steps and
  printed the same score; the guest that also commits it takes 1,062,488,466.

**Reproducing it takes more setup than the C examples** — a nightly Rust toolchain, a
custom target spec, `-Z build-std`, and the `tokenizers` crate vendored with default
features off. See [INSTALL.md § Rust](INSTALL.md#5-rust--only-to-rebuild-the-ml-guests).

```bash
examples/rust/bert-riscv/build.sh

M=.models/minilm-l6
./scripts/run-elf.sh \
    examples/rust/bert-riscv/target/riscv64imafd-unknown-linux-gnu/release/myriscvcandleapp \
    config.json=$M/config.json tokenizer.json=$M/tokenizer.json model.safetensors=$M/model.safetensors
```

The model files are not in the repository. Fetch them from
[`sentence-transformers/all-MiniLM-L6-v2`](https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2)
or any BERT checkpoint candle can load (`./scripts/model-bench.sh --fetch` gets five into
`.models/`). Each `guestName=hostPath` stages a file under the exact name the guest opens.

The same guest works with the other two tools. `--load-file guestName=hostPath` stages a
file under the name the guest opens, so the checkpoint can stay in `.models/`:

```bash
ELF=examples/rust/bert-riscv/target/riscv64imafd-unknown-linux-gnu/release/myriscvcandleapp
M=.models/minilm-l6
FILES="--random-file rvgo/random --load-file config.json=$M/config.json \
       --load-file tokenizer.json=$M/tokenizer.json --load-file model.safetensors=$M/model.safetensors"

bin/mlvm xray --elf $ELF $FILES --svg bert.svg      # every instruction, Rust names demangled
bin/mlvm liar --elf $ELF $FILES                     # one machine lies once; find the instruction
bin/mlvm liar --elf $ELF $FILES --lie output --forge "score: 0.61=score: 0.91"
```

`scripts/gallery.sh --all` runs both on all five checkpoints; the results are in
[GALLERY.md](GALLERY.md).

---

## Next

- [ARCHITECTURE.md](ARCHITECTURE.md) — how the VM and its proofs work
- [ISA.md](ISA.md) — instruction coverage and the known gaps
- [SYSCALLS.md](SYSCALLS.md) — the OS surface
- [BENCHMARKS.md](BENCHMARKS.md) — the measurements behind the numbers above
