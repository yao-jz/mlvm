# Examples

Everything here is a real program that runs **inside** the MLVM RISC-V virtual machine.

```bash
make examples                              # cross-compile all the C examples
./scripts/run-elf.sh examples/build/hello  # run one
```

One-command demo (embedded TinyStories, no extra files):

```bash
make demo                 # bin/mlvm demo --verify
make tour                 # longer hello/mlp/determinism/proof walkthrough
bin/mlvm demo "Once upon a time"
bin/mlvm demo --demo mlp  # 16→32→32→4 network fallback
```

---

## What you need

| To run… | You need |
| --- | --- |
| The pre-built programs (`rvgo/hello-libc`, `tests/riscv-tests/*`) | **Go 1.23+** only |
| The C examples in this folder | + a **RISC-V cross toolchain** |
| The Rust ML guests in `rust/` | + **nightly Rust** and model weights |

`make doctor` from the repo root tells you what you have.
[docs/INSTALL.md](../docs/INSTALL.md) covers installing the rest.

---

## The C examples

Five programs, ordered so each one shows something the previous one didn't. All of them
build with `make examples` and run in under a second.

| Program | Steps | What it demonstrates |
| --- | ---: | --- |
| [`hello.c`](hello.c) | 218 K | A statically linked **glibc** binary boots and prints. Almost all of those instructions are libc start-up, exercising the implemented syscall surface. |
| [`ieee.c`](ieee.c) | 263 K | A determinism check: about 16,000 dependent float operations including gradual underflow, bit-identical to the tested host FPU. |
| [`matmul.c`](matmul.c) | 487 K | A 32×32 double-precision GEMM — 142,459 float instructions, 29% of the trace. The profile a real inference workload has. |
| [`mlp.c`](mlp.c) | 289 K | A **neural network forward pass**: 16→32→32→4, tanh hidden layers, softmax head. |
| [`fileio.c`](fileio.c) | 237 K | Reads a file through the **in-memory filesystem** — how a model checkpoint reaches a guest that cannot touch the outside world. |

### Running them

```bash
# the basics
./scripts/run-elf.sh examples/build/hello
./scripts/run-elf.sh examples/build/mlp

# staging a file into emulated memory (note: the guest opens it by this exact path)
./scripts/run-elf.sh examples/build/fileio examples/data/sample.txt

# see every syscall the guest makes
TRACE=1 ./scripts/run-elf.sh examples/build/hello

# keep the state/proof JSON afterwards
KEEP=1 ./scripts/run-elf.sh examples/build/matmul
```

Every run ends with an instruction profile:

```
=== instruction profile =========================
  steps                  289482
  integer ALU            154139 ( 53.2%)
  memory load/store       49979 ( 17.3%)
  float ALU               11234 (  3.9%)
  float load/store         6227 (  2.2%)
  pages touched            1527  (6.0 MiB)
  throughput              24.72 Minstr/s
=================================================
```

### Building

```bash
make examples                              # all of them
make -C examples CROSS=riscv64-linux-gnu-  # if your toolchain has a different prefix
make -C examples native                    # also build host binaries, for diffing
make -C examples dump                      # disassemble, useful when debugging a crash
make -C examples clean
```

---

## Writing your own

Drop a `.c` file in this directory. The Makefile globs `*.c`, so `make examples` picks it
up with the right flags automatically.

```c
// examples/mine.c
#include <stdio.h>
int main(void) { printf("%d\n", 6 * 7); return 0; }
```

```bash
make examples && ./scripts/run-elf.sh examples/build/mine
```

Or compile by hand:

```bash
riscv64-unknown-linux-gnu-gcc -O2 -static \
    -march=rv64imafd -mabi=lp64d -ffp-contract=off \
    -o mine mine.c -lm
./scripts/run-elf.sh mine
```

### The three flags that matter

| Flag | Why |
| --- | --- |
| `-march=rv64imafd` | **No `c`.** The compressed extension emits 2-byte instructions this VM does not decode. An RVC binary reverts at step 0 with `unknown instruction opcode: 1`. |
| `-static` | There is no dynamic loader inside the VM. |
| `-ffp-contract=off` | Keeps native and RISC-V comparison builds from making different contraction choices. MLVM's FMADD implementation is fused. |

### When something fails

| Error | Cause |
| --- | --- |
| `unknown instruction opcode: 1` | RVC. Rebuild without `c` in `-march`. |
| `unrecognized system call: N` | Not implemented. Run with `TRACE=1` to see the lead-up, then see [SYSCALLS.md](../docs/SYSCALLS.md#adding-a-syscall). |
| `file "x" was not staged into the VM` | Pass it: `./scripts/run-elf.sh mine x` |
| Runs forever | Check progress with `INFO_AT='%1000000'`. Some libc paths (locale, `iconv`) are enormous. |

---

## Verifying, not just running

Three scripts turn these programs into evidence rather than demos.

```bash
make determinism     # compile the same C twice, run one in the VM and one natively, diff
make proof           # select a trace step and emit its state witness and memory branch
make bench           # throughput, witness size and instruction mix for every example
```

`determinism` is the interesting one. `ieee` and `matmul` come out **bit-identical**;
`mlp` deliberately does not, because it calls `tanh` and `exp` from libm — and that
divergence is a property of the C library, not of the emulator.
[BENCHMARKS.md](../docs/BENCHMARKS.md#floating-point-determinism) walks through the
decomposition.

---

## The Rust ML guests — [`rust/`](rust/)

Heavier, and they need a nightly toolchain plus model weights.

| Crate | What it is |
| --- | --- |
| [`rust/bert-riscv`](rust/bert-riscv) | **Sentence similarity with BERT-family encoders** (22.7M–110M parameters), a [candle](https://github.com/huggingface/candle)-based guest cross-compiled to RV64IMAFD and run entirely inside the VM. Workload demo: it prints its score rather than committing it |
| [`rust/bert-host`](rust/bert-host) | The same program built for the host, as a reference |
| [`rust/candle-reference`](rust/candle-reference) | Native candle reference implementation, used for output comparison |
| [`rust/float-placement`](rust/float-placement) | One `no_std` GEMM kernel built for rv64imafd and rv64ima, to compare interpreter and compiler float emulation |
| [`rust/float-coprocessor`](rust/float-coprocessor) | The RV64IMA nested machine behind the experimental `f64` sub-game |
| [`rust/playground`](rust/playground) | Small guests for probing VM behaviour; targets RV64**IMA** (no F/D) so the compiler emits soft-float calls |

See [`rust/README.md`](rust/README.md) for the build and run instructions.

---

## Where to go next

| | |
| --- | --- |
| [docs/EXAMPLES.md](../docs/EXAMPLES.md) | narrated walkthrough of each example, with output and analysis |
| [docs/ARCHITECTURE.md](../docs/ARCHITECTURE.md) | how the VM and its proof components work |
| [docs/SYSCALLS.md](../docs/SYSCALLS.md) | the OS surface these programs run on |
| [docs/ISA.md](../docs/ISA.md) | which instructions are supported |
