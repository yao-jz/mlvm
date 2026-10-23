# Installation

MLVM has a small required core and several optional layers. Install only the layer you
need — running the VM needs nothing but Go.

| I want to… | I need |
| --- | --- |
| Build and run the VM, run the conformance suites | **Go 1.23+**, Python 3.8+ |
| Write my own guest programs in C | + a **RISC-V cross toolchain** |
| Work on the on-chain verifier | + **Foundry** (`forge`) |
| Rebuild the Rust ML guests (BERT, candle) | + **Rust nightly** with a `riscv64` target |

`make doctor` reports what you have and what is missing:

```
$ make doctor
required
  go (>= 1.23)                 go1.27.2
  python3                      3.11.5

to build guest programs
  riscv64 gcc                  riscv64-unknown-linux-gnu-gcc 13.2.0

to work on the on-chain verifier
  forge (foundry)              0.2.0

to rebuild the Rust ML guests
  rustc (nightly)              1.84.0-nightly
  riscv rust target            installed
```

---

## 1. Clone

```bash
git clone https://github.com/yao-jz/mlvm.git
cd mlvm
```

The submodules (`forge-std`, `optimism`) are only needed for the Solidity side. If you
already cloned without them:

```bash
git submodule update --init --recursive
```

Note that `lib/optimism` is a large repository. Skip it entirely unless you plan to work
on `rvsol`: the Go VM, examples, and fast conformance tests work without it, while the
Solidity and embedded-EVM tests require its contracts and build artifacts.

## 2. Go

The VM, the CLI and the test suites are Go.

```bash
# macOS
brew install go

# Debian / Ubuntu
sudo apt install golang-go        # or fetch a newer build from https://go.dev/dl/

# verify
go version                        # want >= 1.23
```

Then:

```bash
make check       # formatting, docs, shell, vet, and all default Go tests
make build       # -> bin/mlvm
bin/mlvm --version
```

If `make check` passes you have a working core VM.

<details>
<summary><strong>macOS: "Killed: 9" with no error message</strong></summary>

Go's linker produces an ad-hoc *linker-signed* Mach-O signature that some macOS security
configurations refuse. The binary dies with `SIGKILL` and prints nothing, which is a
confusing failure mode. `make build` already re-signs to work around it:

```bash
codesign -s - -f bin/mlvm
```

If you build by hand with `go build`, run that afterwards.
</details>

## 3. RISC-V cross toolchain

Needed to compile your own guest programs. **MLVM ships pre-built guest binaries**
(`rvgo/hello-libc`, the `tests/riscv-tests` suites), so you can explore without
this — but `make examples` needs it.

### The requirement

MLVM executes **RV64IMAFD**. Your toolchain must be able to target:

```
-march=rv64imafd -mabi=lp64d
```

The critical part is what is *absent*: no `c`. The compressed extension emits 2-byte
instructions that this VM does not decode, and an RVC binary fails at step 0 with
`unknown instruction opcode: 1`. A `rv64gc` toolchain is fine as long as you pass
`-march=rv64imafd` when you compile.

### macOS

```bash
brew tap riscv-software-src/riscv
brew install riscv-gnu-toolchain
```

Or build it yourself for full control over the ABI:

```bash
git clone https://github.com/riscv-collab/riscv-gnu-toolchain
cd riscv-gnu-toolchain
./configure --prefix=/opt/riscv --with-arch=rv64imafd --with-abi=lp64d
make linux -j$(sysctl -n hw.ncpu)
export PATH=/opt/riscv/bin:$PATH
```

<details>
<summary><strong>macOS build failures: bison and make versions</strong></summary>

macOS ships an ancient `bison` and BSD `make` at `/usr/bin`, and the toolchain build
requires GNU versions:

```bash
brew install bison make
brew link bison --force
ln -s /opt/homebrew/bin/gmake /usr/local/bin/gnumake
```

`/usr/local/bin` must come *before* `/usr/bin` on your `PATH`:

```bash
export PATH="/usr/local/bin:$PATH"     # add to ~/.zshrc
```

If configure cannot find GMP or MPFR:

```bash
./configure --prefix=/opt/riscv \
  --with-gmp=$(brew --prefix gmp) \
  --with-mpfr=$(brew --prefix mpfr)
```
</details>

### Debian / Ubuntu

```bash
sudo apt install gcc-riscv64-linux-gnu
```

That installs the prefix `riscv64-linux-gnu-` rather than `riscv64-unknown-linux-gnu-`, so:

```bash
make -C examples CROSS=riscv64-linux-gnu-
```

### Verify

```bash
make examples
./scripts/run-elf.sh examples/build/hello
```

You should see the program's output followed by an instruction profile.

## 4. Foundry — only for the on-chain verifier

```bash
curl -L https://foundry.paradigm.xyz | bash
foundryup

cd rvsol
forge build
forge test
```

`forge build` produces the artifacts in `rvsol/out/` that the Go EVM tests read. The
integer stepper and float boundary contracts are experimental and unaudited; see
[Project Status](STATUS.md) before relying on their scope.

## 5. Rust — only to rebuild the ML guests

The BERT inference guest (`examples/rust/bert-riscv`) is built with a **custom target spec** and
`-Z build-std`, both of which require nightly.

```bash
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
rustup toolchain install nightly
rustup default nightly
rustup component add rust-src            # required by -Z build-std
rustup target add riscv64gc-unknown-linux-gnu
```

The target specs live next to each crate:

| File | ISA | ABI | Used by |
| --- | --- | --- | --- |
| `examples/rust/bert-riscv/riscv64imafd-unknown-linux-gnu.json` | `+m,+a,+f,+d` | `lp64d` | BERT inference, hardware floats |
| `examples/rust/playground/riscv64ima-unknown-linux-gnu.json` | `+m,+a` | `lp64` | soft-float experiments |

Both set `"crt-objects-fallback": "false"` and are linked statically — there is no dynamic
loader inside the VM.

```bash
cd examples/rust/bert-riscv
./build.sh              # pass 1 to cargo clean first
```

`build.sh` invokes:

```bash
cargo build --target riscv64imafd-unknown-linux-gnu.json \
            -Zbuild-std=std,panic_abort --release
```

<details>
<summary><strong>Why <code>-Zbuild-std</code>?</strong></summary>

A custom target spec has no pre-compiled `std`, so Cargo has to build the standard library
from source for it. That is what `-Zbuild-std=std,panic_abort` does, and why `rust-src`
must be installed.
</details>

<details>
<summary><strong>"multiple definition of __adddf3"</strong></summary>

When linking soft-float builds, both `compiler_builtins` and libgcc provide the soft-float
helpers. `.cargo/config.toml` already handles it:

```toml
rustflags = ["-C", "link-arg=-Wl,--allow-multiple-definition"]
```
</details>

The Rust guests also need `tokenizers` vendored at `third_party/tokenizers` with
`default-features = false` — the default feature set pulls in `onig`, which needs a C
regex engine and network access at build time. It is not a submodule because only this one
guest needs it; clone [huggingface/tokenizers](https://github.com/huggingface/tokenizers)
there if you want to rebuild the BERT guest.

## 6. Optional: RISC-V conformance test vectors

The repository already contains the compiled test suites under
`tests/riscv-tests/`. To regenerate them:

```bash
git clone https://github.com/riscv-software-src/riscv-tests
cd riscv-tests
git submodule update --init --recursive
autoconf
./configure --prefix=$RISCV/target
RISCV_PREFIX=/opt/riscv/bin/riscv64-unknown-elf- make

cp isa/rv64ui-p-*  <mlvm>/tests/riscv-tests/rv64ui-p/
cp isa/rv64um-p-*  <mlvm>/tests/riscv-tests/rv64um-p/
cp isa/rv64ua-p-*  <mlvm>/tests/riscv-tests/rv64ua-p/
cp isa/rv64u[fd]-p-* isa/rv32u[fd]-p-* <mlvm>/tests/riscv-tests/rv64fd/
```

---

## Troubleshooting

**`unknown instruction opcode: 1` at step 0**
Your binary uses the compressed extension. Rebuild with `-march=rv64imafd` (no `c`).

**`file "x" was not staged into the VM`**
The guest called `openat("x")` but nothing put `x` in emulated memory. Pass it:
`./scripts/run-elf.sh prog x`. The guest must open the file under the exact string it was
staged with — there is no path resolution inside the VM.

**`unrecognized system call: N`**
The guest used a syscall MLVM does not implement. `docs/SYSCALLS.md` lists the 37 syscall
numbers the fast VM recognises; adding one is usually a small case in `fast/vm.go`, plus
mirrors in `slow/vm.go` and `RISCV.sol` if it must be checkable. Run with `--trace` to see
the full syscall stream leading up to it.

**`failed to open randomness file "..."`**
An explicit `--random-file` path does not exist. Fix the path or omit the flag to use the
default sparse, zero-filled deterministic input.

**The VM prints thousands of `syscall:` lines**
That trace is off by default; something enabled it. Drop `--trace`, or unset `TRACE=1`.

---

## Next

- [`ARCHITECTURE.md`](ARCHITECTURE.md) — how the VM, memory merkleization and proofs work
- [`EXAMPLES.md`](EXAMPLES.md) — guided walkthroughs, from hello-world to BERT
- [`ISA.md`](ISA.md) — exactly which instructions are supported, and the known gaps
- [`SYSCALLS.md`](SYSCALLS.md) — the syscall surface, memory map and in-memory filesystem
