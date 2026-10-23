# tinystories-candle

The TinyStories-260K language model, generating text through Hugging Face
[Candle](https://github.com/huggingface/candle) inside MLVM.

It is the same model, tokenizer and request as the C decoder in
[`examples/decoder`](../../decoder), the guest `mlvm demo` runs, implemented a second time:
the llama2.c checkpoint is loaded into `candle-transformers`' `llama2_c::Llama`, and every
tensor operation goes through Candle and its `gemm` kernels. The prompt is tokenized with
the same byte-pair encoder (ported from llama2.c's `run.c`), tokens are chosen greedily, and
the answer is committed at `MLVM_OUT` in the decoder's layout
([DECODER-PROTOCOL.md](../../../docs/DECODER-PROTOCOL.md)). So the two programs can be
compared token for token.

| Prompt | C decoder | Candle | Same 16 tokens |
| --- | ---: | ---: | --- |
| *Once upon a time* | 38,186,542 instructions | 122,197,182 instructions | yes |
| *The dragon* | 41,736,368 | 133,767,232 | yes |
| *Lily and Ben* | 38,115,752 | 122,338,820 | yes |

Two independent implementations, one with fused multiply-adds in hand-written C loops and
one with separate multiplies and adds in a general-purpose tensor library, choose the same
16 tokens on all three prompts. Candle takes 3.2 times the instructions.

## What X-ray shows

`gemm` does 41% of the work. Much of the rest is the cost of a general tensor library on a
tiny model: allocating, copying and freeing small tensors (`from_iter`,
`copy_strided_src`, `malloc`, `_int_free`, `memcpy`). And 7.7%, 9.5 million instructions,
is `num_cpus::get_num_cpus`: Candle asks how many CPUs there are on every `matmul`
(`candle_core::utils::get_num_threads`) unless `RAYON_NUM_THREADS` is set, and inside the VM
there is no environment. Setting it at startup (`std::env::set_var("RAYON_NUM_THREADS",
"1")`) takes the run from 122,197,182 to 113,196,552 instructions with the same tokens. The
guest is left unchanged here, so the profile shows the finding. On the BERT guest, whose
matrices are larger, the same call is 0.05%.

## Building and running

Same toolchain as [`../bert-riscv`](../bert-riscv): nightly Rust with `rust-src` and a
riscv64 glibc cross linker ([INSTALL.md](../../../docs/INSTALL.md)). Then, from the
repository root:

```bash
examples/rust/tinystories-candle/build.sh
python3 examples/decoder/mkrequest.py "Once upon a time" -o /tmp/request.bin

ELF=examples/rust/tinystories-candle/target/riscv64imafd-unknown-linux-gnu/release/tinystories-candle
FILES="--load-file model.bin=models/stories260K.bin --load-file tokenizer.bin=models/tok512.bin \
       --load-file request.bin=/tmp/request.bin"

bin/mlvm xray --elf $ELF $FILES --svg candle.svg       # per-token timeline included
bin/mlvm liar --elf $ELF $FILES                        # one ULP somewhere in Candle
bin/mlvm liar --elf $ELF $FILES --lie token --word Tim # forge the committed answer
```

Because it stages the TinyStories tokenizer as `tokenizer.bin`, `mlvm liar` shows its
committed tokens as text and accepts `--lie token`, as for the embedded decoder.
