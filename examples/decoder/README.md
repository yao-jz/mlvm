# TinyStories decoder guest

A karpathy/llama2.c-style RV64IMAFD guest for MLVM.

```bash
make                                 # cross-compile
make native request                  # host smoke + sample request.bin
./scripts/decoder-fetch.sh           # from repo root (maintainers)
bin/mlvm demo "Once upon a time"     # uses the embedded 260K pack
```

See [docs/DECODER-PROTOCOL.md](../../docs/DECODER-PROTOCOL.md) for the committed
`request.bin` / `MLResult` layouts.
