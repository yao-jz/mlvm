# Embedded demo assets

| File | Role |
| --- | --- |
| `decoder-rv64imafd.elf` | TinyStories Llama2-C guest; default `mlvm demo` |
| `stories260k.bin` | Pinned karpathy/tinyllamas 260K checkpoint (MIT) |
| `tok512.bin` | Matching tokenizer |
| `mlp-rv64imafd.elf` | Fallback `mlvm demo --demo mlp` guest |

Rebuild guests with a pinned RISC-V toolchain:

```bash
make -C examples/decoder
cp examples/decoder/build/decoder rvgo/internal/e2edemo/assets/decoder-rv64imafd.elf
make -C examples CROSS=riscv64-unknown-linux-gnu-
cp examples/build/mlp rvgo/internal/e2edemo/assets/mlp-rv64imafd.elf
```

Weights are copied from `./scripts/decoder-fetch.sh` (`stories260K.bin` / `tok512.bin`).
CI compares the embedded decoder SHA against a rebuild when a cross-compiler is available.
