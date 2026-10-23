# llm-candle

A Hugging Face language model generating text through
[Candle](https://github.com/huggingface/candle) inside MLVM.

The guest loads a Llama-architecture checkpoint with `candle-transformers`'
`llama::Llama`, tokenizes the prompt with the checkpoint's own `tokenizer.json` (Hugging
Face `tokenizers`), and generates greedily with a KV cache. It was run with
[HuggingFaceTB/SmolLM2-135M](https://huggingface.co/HuggingFaceTB/SmolLM2-135M) (Apache-2.0):
30 layers, hidden size 576, 9 attention heads sharing 3 key/value heads, a 49,152-token
byte-level BPE vocabulary, tied embeddings, and bf16 weights that Candle converts to f32
when it loads them.

| Prompt | Generated (16 tokens) | Instructions | Same tokens as native Candle |
| --- | --- | ---: | --- |
| *Once upon a time* | ", there was a little girl named Lily. She lived in a big house with" | 14,764,379,315 | yes |
| *def fibonacci(n):* | `\n    if n == 0:\n        return 0\n    elif n == ` | 15,662,424,162 | yes |

"Native" is the same source built for macOS arm64 and run there, where Candle's `gemm`
uses NEON and several threads. Inside the VM it runs single-threaded on MLVM's SoftFloat.
The two choose the same 16 tokens for both prompts.

The request and the committed answer use the TinyStories decoder's layouts
([DECODER-PROTOCOL.md](../../../docs/DECODER-PROTOCOL.md)): the prompt and token budget in
`request.bin`, the generated token IDs at `MLVM_OUT`. So `mlvm xray` marks each token as it
is printed, and `mlvm liar` can forge one (`--lie token --token-id`).

## What X-ray shows

On *Once upon a time*: loading the checkpoint and reading the prompt is 3.0 billion
instructions, the prompt's forward pass brings the first token at 5.1 billion, and each
generated token after that costs 640 million. `gemm` is 76% of the run: `gemm_basic` 63.4%,
the f32 microkernel 10.1% and operand packing 2.9%. Single-precision arithmetic outnumbers
double 366 to 1.

Most of the loading is not reading the file but converting it: 13% of the whole run is
`Tensor::to_dtype` turning the bf16 weights into f32, about 12 instructions per weight. The
tied embedding matrix is converted twice, 2.3% each time, once as the embedding and once as
the output projection, because candle-transformers 0.4.1 loads them separately.

## Fetching the checkpoint

The weights are not in the repository. From the repository root:

```bash
mkdir -p .models/smollm2-135m
for f in config.json tokenizer.json model.safetensors; do
  curl -fL -o .models/smollm2-135m/$f https://huggingface.co/HuggingFaceTB/SmolLM2-135M/resolve/main/$f
done
```

## Building and running

Same toolchain as [`../bert-riscv`](../bert-riscv): nightly Rust with `rust-src`, a riscv64
glibc cross linker, and `third_party/tokenizers` ([INSTALL.md](../../../docs/INSTALL.md)).

```bash
examples/rust/llm-candle/build.sh
python3 examples/decoder/mkrequest.py "Once upon a time" -o /tmp/request.bin

ELF=examples/rust/llm-candle/target/riscv64imafd-unknown-linux-gnu/release/llm-candle
M=.models/smollm2-135m
FILES="--random-file rvgo/random --load-file config.json=$M/config.json \
       --load-file tokenizer.json=$M/tokenizer.json --load-file model.safetensors=$M/model.safetensors \
       --load-file request.bin=/tmp/request.bin"

bin/mlvm xray --elf $ELF $FILES --svg smollm2.svg
bin/mlvm liar --elf $ELF $FILES                                       # one ULP in gemm
bin/mlvm liar --elf $ELF $FILES --lie token --token-index 7 --token-id 6362   # "Lily" -> "Tim"
```

A run takes about 15 billion instructions: roughly 9.5 minutes of CPU and 3 to 4 GB of
memory. A dispute runs it on two machines and replays parts of it while bisecting: 25 to 37
minutes of CPU and about 10 GB at peak ([GALLERY.md](../../../docs/GALLERY.md#a-hugging-face-llm-smollm2-135m)).

Other Llama-architecture checkpoints that `candle-transformers` 0.4.1 can load should work
the same way, as long as their token IDs fit in 16 bits; only SmolLM2-135M has been run.
