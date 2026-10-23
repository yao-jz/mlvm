# Rust ML guests

These workload demonstrations run selected machine-learning models inside the fast VM using
[candle](https://github.com/huggingface/candle), HuggingFace's Rust ML framework. They
characterize execution scale and ISA demand; their full traces are not currently settleable.

They need more setup than the [C examples](../README.md): a nightly Rust toolchain, a
custom target spec, `-Z build-std`, and model weights. Start with the C examples if you
just want to see the VM work.

---

## The crates

| Crate | Target | What it does |
| --- | --- | --- |
| [`bert-riscv`](bert-riscv) | `riscv64imafd` | Sentence similarity with BERT-family encoders (22.7M–110M parameters), **inside the VM** |
| [`bert-host`](bert-host) | host | The same program, natively — the reference output |
| [`llm-candle`](llm-candle) | `riscv64imafd` | A Hugging Face LLM (SmolLM2-135M, Llama architecture) generating text through Candle **inside the VM**: the same tokens as native Candle |
| [`tinystories-candle`](tinystories-candle) | `riscv64imafd` | TinyStories-260K generating text through Candle **inside the VM**: the same tokens as the C decoder, 3.2× the instructions |
| [`candle-reference`](candle-reference) | host | Fuller native candle harness, builds against `third_party/candle` |
| [`float-placement`](float-placement) | both | One `no_std` GEMM kernel built for **both** targets. Answers whether floating point is cheaper to emulate in the interpreter or in the compiler — `make float-placement` |
| [`float-coprocessor`](float-coprocessor) | `riscv64ima` | **One IEEE-754 operation as RV64I instructions.** Experimental nested terminal component used by `make float-dispute` |
| [`playground`](playground) | `riscv64ima` | Small probes, integer-only target |

---

## Setup

```bash
# 1. nightly Rust with the standard library source
rustup toolchain install nightly
rustup default nightly
rustup component add rust-src

# 2. a RISC-V cross linker (see ../../docs/INSTALL.md)
riscv64-unknown-linux-gnu-gcc --version

# 3. submodules: candle and tokenizers
git submodule update --init third_party/candle third_party/tokenizers
```

### The tokenizers dependency

`bert-riscv` needs the `tokenizers` crate built **without default features** — the defaults
pull in `onig`, a C regex engine that needs network access at build time and does not
cross-compile cleanly.

The submodule pins `third_party/tokenizers` to the commit these guests were built with
(`b62132e`, 0.23.2-dev), and each `Cargo.toml` already points at
`third_party/tokenizers/tokenizers` with `default-features = false`.

---

## Building and running

```bash
cd examples/rust/bert-riscv
./build.sh                 # or ./build.sh clean
```

Which runs:

```bash
cargo build --target riscv64imafd-unknown-linux-gnu.json \
            -Zbuild-std=std,panic_abort --release
```

Then, from the repo root, staging the model files into emulated memory:

```bash
./scripts/run-elf.sh \
    examples/rust/bert-riscv/target/riscv64imafd-unknown-linux-gnu/release/myriscvcandleapp \
    config.json tokenizer.json model.safetensors
```

The guest opens those files by **exactly** those names, so they must sit at the repo root
under those paths. Fetch them from
[`sentence-transformers/all-MiniLM-L6-v2`](https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2)
or any BERT checkpoint candle can load.

### Measured results

Five checkpoints, all through the same guest, all inside the VM
(`make models`, full data in `results/model-bench.tsv`):

| Model | Params | Steps | Float % | Memory | Wall clock | Score |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `all-MiniLM-L6-v2` | 22.7M | 925M | 57.7% | 285 MiB | 47 s | `0.6110916734` |
| `bge-small-en-v1.5` | 33.4M | 1,697M | 63.0% | 408 MiB | 87 s | `0.7658209205` |
| `all-MiniLM-L12-v2` | 33.4M | 1,690M | 63.1% | 407 MiB | 87 s | `0.6257025003` |
| `bge-base-en-v1.5` | 109.5M | 6,061M | 69.0% | 1,280 MiB | 309 s | `0.7760061622` |
| `bert-base-uncased` | 110.1M | 6,107M | 68.5% | 1,288 MiB | 309 s | `0.6334546804` |

Input in every case: *"The cat sits outside"* vs *"The cat plays in the garden"*.

The score is this guest's own, not the reference `sentence-transformers` similarity: the
guest encodes sentences one at a time, renames legacy checkpoint keys, and mean-pools over
every token *including padding*, with no attention mask. It is printed to stdout and, since
2026-10-20, also committed at `MLVM_OUT` in the layout of `examples/mlvm_result.h`, so the
post-state binds it. Each row is a single run recorded with an earlier VM build and guest;
the current build caps staged-file reads at 32 bytes, so step counts are now higher. See
[BENCHMARKS.md](../../docs/BENCHMARKS.md#limitations-of-the-model-measurements).

`make model-determinism` runs the same checkpoints natively through
[`bert-host`](bert-host) — byte-identical `src/main.rs`, the same locked Candle version,
but a different target, `std`, libm and code generation — and compares. That makes it a
cross-build portability comparison rather than a test of the VM alone. Across all five
models the scores land within **3 to 19 ULP of float32** — and the error does not grow with
trace length: `bert-base-uncased` runs 6.6× the instructions of `all-MiniLM-L6-v2` and lands
at the same 3 ULP. MLVM's FMADD path is now fused; remaining differences come from what
differs between the two builds, chiefly transcendental libraries and code generation. See
[BENCHMARKS.md](../../docs/BENCHMARKS.md#determinism-on-real-models).

### Porting notes

Two things had to change for real Rust ML guests to work, and you will hit both:

**Parallel iterators.** `tokenizers::encode_batch` uses rayon, which builds a global thread
pool on first use. The VM refuses `clone(2)` on purpose — concurrent execution has no
deterministic interleaving to prove — so the pool cannot be built and the call panics with
`ThreadPoolBuildError { IOError(PermissionDenied) }`. Encoding sequentially gives identical
token IDs. Expect this anywhere a framework reaches for a parallel iterator.

**Checkpoint naming.** Task-head checkpoints keep the encoder under a `bert.` prefix, and
the 2018 BERT release names LayerNorm parameters `gamma`/`beta` instead of `weight`/`bias`.
The guest normalises both, which is why `bert-base-uncased` loads alongside modern
sentence-transformers checkpoints.

---

## Why the custom target specs

`rustc` has no built-in target for RV64IMAFD-with-glibc, so each crate ships a JSON spec:

| Spec | ISA | ABI | Purpose |
| --- | --- | --- | --- |
| `bert-riscv/riscv64imafd-unknown-linux-gnu.json` | `+m,+a,+f,+d` | `lp64d` | hardware float instructions, executed by the VM's SoftFloat |
| `playground/riscv64ima-unknown-linux-gnu.json` | `+m,+a` | `lp64` | no float instructions; the compiler expands them into integer soft-float calls |

That pair is the basis of two crates.

[`float-placement`](float-placement) builds one `no_std`
kernel both ways and runs both in the same VM:

| Build | Float instructions | Steps |
| --- | ---: | ---: |
| `rv64imafd` — VM executes `FADD.D` | 66 | **903,446** |
| `rv64ima` — compiler expands to integer code | 0 | **22,898,400** |

**25.3× more steps**, ~91 integer instructions per double-precision operation, bit-identical
results. `make float-placement` reproduces it.

[`float-coprocessor`](float-coprocessor) explores a terminal mechanism based on the same
fact. `RISCV.sol` executes RV64IMA and cannot execute `FADD.D`, while an operation built for
`riscv64ima` contains only integer instructions the stepper already supports. The prototype
derives a nested machine from operands, locally searches its trace, and executes one nested
step in an EVM. It always rounds to nearest even: its ABI has no rounding-mode input, so a
`D` instruction using `rtz`, `rdn`, `rup`, `rmm` or a non-zero dynamic `frm` is outside its
scope.

Two design constraints make it work, and both are unusual:

- **No arguments, no environment, no stack-passed data.** Operands are read from fixed
  absolute addresses, so the nested pre-state is a pure function of `(image, op, a, b)` and
  independent implementations can derive byte-identical states. The current contract does
  not bind those caller-supplied operands to an outer instruction witness.
- **No floating-point instructions, checked rather than assumed.** A build containing one
  would run correctly off chain and nothing would notice — but its arithmetic would be a
  single `FADD.D` again. `fast.assertNoFloatInstructions` scans every executable section and
  rejects the image.

`make float-dispute` exercises this experimental terminal route using an `FMUL.D` selected
from the C MLP and prints the missing outer-game boundaries.
`make float-subgame` checks the nested machine against `rvgo/comp` on 1,522 vectors.

Because a custom spec has no pre-built `std`, Cargo has to compile the standard library
from source, which is what `-Zbuild-std=std,panic_abort` does and why `rust-src` is
required.

---

## Troubleshooting

**`multiple definition of __adddf3`** — both `compiler_builtins` and libgcc provide the
soft-float helpers. `.cargo/config.toml` already passes
`-C link-arg=-Wl,--allow-multiple-definition`.

**`can't find crate for std`** — `rustup component add rust-src`, and make sure you are on
nightly.

**`unknown instruction opcode: 1`** — something in the build emitted compressed
instructions. Check that the target spec's `features` string has no `c`.

**Guest reverts on `openat`** — the model files were not staged, or were staged under a
different name than the guest opens. Names are compared verbatim; there is no path
resolution inside the VM.
