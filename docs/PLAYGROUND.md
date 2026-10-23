# Playground, `mlvm liar` and `mlvm xray`

Three demonstrations of one property: MLVM commits every step of a run, so the run can be
compared, disputed and profiled exactly. Each has a CLI command and an act in the browser
playground, which runs the same Go code compiled to WebAssembly.

| | CLI | Browser | What it shows |
| --- | --- | --- | --- |
| Act 1 | `mlvm demo` | Run | A language model inside the VM; the state root is the same on every machine. |
| Act 2 | `mlvm liar` | Catch the liar | Bisection over state hashes finds one lied-about instruction among 38 million. |
| Act 3 | `mlvm xray` | X-ray | An exact, instruction-level profile: function, call stack, class. |

The maturity boundary for all three is in [STATUS.md](STATUS.md): they are tested local
demonstrations, not a deployed dispute protocol.

## Act 1: the same root everywhere

The guest is `examples/decoder`, a llama2.c-style C program compiled for RV64IMAFD, with the
TinyStories-260K checkpoint and tokenizer staged as files. The prestate root hashes the ELF,
the staged files and the request; the poststate root commits the halted machine, including
the generated token IDs, which the guest writes to the fixed `MLVM_OUT` window.

`.github/workflows/determinism.yml` runs five prompts on linux/amd64, linux/arm64,
darwin/arm64, darwin/amd64, windows/amd64 and WebAssembly (Node), and fails unless all
six produce the same prestate, step count and poststate. The merged table is published
with the playground as `determinism.json`, and `badge.json` feeds the README badge. The
playground compares your browser's root with that table, and a share link carries a prompt
and root so the recipient's machine checks it.

## Act 2: catch the liar

`rvgo/internal/liar` implements the dispute; `web/src/dispute.js` is the same loop
in JavaScript for the browser.

1. **Two parties run the program.** Each saves a copy of its machine every 4,194,304
   instructions (`--checkpoint-interval`) and can then answer "what is your state hash
   after n instructions?" by replaying from the nearest copy. Copies share memory pages
   with the running machine until one side writes a page (copy-on-write), so a copy costs
   the pages written since the one before, not the whole guest. A party keeps at most 64
   copies: past that it doubles its interval and drops every other copy, so a
   billion-instruction BERT guest keeps 64 copies about 17 million instructions apart.
2. **The liar deviates once.**
   - `--lie ulp` flips the lowest mantissa bit of one f32 arithmetic result at or after
     `--at` (a step or a percentage). Most such lies do not survive: later rounding makes
     the two machines bit-identical again. So the liar forks an honest shadow at each
     candidate, runs both in lockstep comparing registers and every stored word, abandons
     the lie when they converge, and keeps the first one that is still alive 2.5 million
     instructions later, longer than one forward pass (about 1.8 million). A
     difference that outlives a forward pass has reached state that later passes keep,
     such as the KV cache. That is a heuristic: if the kept lie still vanishes before the
     program halts (more likely with few generated tokens), the liar starts over and lies at
     the next candidate (`liar.RunUntilDisputed`). The report lists every abandoned attempt.
   - `--lie load` flips the lowest bit of the value one `flw`/`fld` brings into a register:
     the machine misreads one weight or activation from memory. It is tried in lockstep
     the same way, because rounding absorbs most of these too.
   - `--lie int` flips the lowest bit of one integer ALU result (not `ra`, `sp`, `gp` or
     `tp`), tried in lockstep too. A kept lie that later crashes the program, or keeps it
     running past twice the honest run, counts as failed, and the liar moves on.
   - `--lie output --forge OLD=NEW` prints `NEW` where the honest machine prints `OLD`.
     Right before the `write(2)` that prints those bytes, the liar changes them in the
     guest's buffer, so bisection lands on the instruction before that `ecall`. Stdout is
     not part of the state, so if the guest overwrites that buffer before it exits, the
     machines end in the same state and there is nothing to dispute.
   - `--lie commit --forge OLD=NEW` forges the answer the guest commits at `MLVM_OUT`
     (`examples/mlvm_result.h`): every store into those bytes leaves the forged value
     behind. `--forge` takes text, or `0x`-prefixed hex for binary answers.
   - `--lie token --token-index 9 --word Tim` replaces the byte the guest stores for one
     generated token, so the committed answer reads "named Tim" instead of "named Lily". It
     works on the decoder and on any guest that stages the TinyStories tokenizer as
     `tokenizer.bin`, such as `examples/rust/tinystories-candle`. `--token-id 6362` forges a
     token ID instead of a word, for any guest that commits token IDs in the decoder's
     layout, such as `examples/rust/llm-candle` with its own tokenizer.

   `--lie ulp` and `--lie load` take f32 results for the decoder and f32 or f64 for other
   guests; `--precision s|d|any`, `--mnemonic fadd.s` and `--function attention` narrow the
   candidates.
3. **Bisection.** The referee asks both parties for their hash at the midpoint of the
   interval in dispute and keeps the half where they first differ. Moves are public, so each
   party keeps its machine at the last agreed step and later questions replay only inside
   the interval. 38,186,542 instructions take 25 or 26 questions.
4. **The referee checks one instruction** from the witness the honest party provides for
   the agreed state (the witness must hash to that state):
   - integer and memory instructions, float loads and stores included: `slow.Step`, and in
     the native CLI also `RISCV.sol`'s compiled bytecode in an in-process EVM
     (`internal/evmstep`). That covers the `load` and `output` lies;
   - floating-point arithmetic (f32 and f64): the referee checks the instruction word's Merkle proof
     against the witness memory root, re-executes that one instruction with SoftFloat from
     the witness registers and `fcsr`, and re-encodes the post-state witness.

The game is not specific to the decoder. `mlvm liar --demo mlp` plays it on the embedded
C MLP, and `mlvm liar --elf program --load-file name=path` on any RV64IMAFD static binary;
for those, the answer shown for each machine is a line of its stdout. The
[gallery](GALLERY.md) runs it on Candle BERT models of one to 6.7 billion instructions.

The browser offers five lies on the decoder: `ulp`, `load` and `int`, aimed by position,
instruction and function; `output`, which prints a word the machine did not compute and so
ends with both machines in the same state; and `token`, which commits one. (`commit` is the
token lie for the decoder, whose committed answer is token IDs.)

What it does not do: there is no chain, no bonds, no clocks and no challenge period. f32
arithmetic is not settleable on chain today. `RISCV.sol` does not implement floating point,
and the experimental nested sub-game covers f64 in round-to-nearest-even only.

## Dispute records: replay and referee

A finished dispute is a record of a few kilobytes
([`mlvm.dispute-record/1`](../spec/dispute-record-v1.md)): the questions and both parties'
answers, and the evidence for the last step, the 674-byte state witness and the Merkle
proofs the disputed instruction consumes. Anyone can rule on it again without running either
machine:

```bash
mlvm liar --lie token --json > dispute.json
mlvm referee dispute.json
```

`mlvm referee` checks that the questions form a bisection that ends on one instruction, that
the witness hashes to the state both parties agreed on before it, at that step, and that the
instruction word is in its Merkle proof, then executes that one instruction: with SoftFloat
for float arithmetic, with `slow.Step` and RISCV.sol's compiled bytecode in an EVM for
everything else. It fails if the record's own ruling differs, and rejects a record with a
changed question, answer, witness byte or proof sibling (`TestRecheckRejectsTamperedRecords`).

The playground uses the same records three ways. Act 2's verdict has a "Download this
dispute" button; the native `mlvm referee` accepts that file and adds the EVM check the
browser leaves out. Each gallery dispute can be replayed, its questions and verdict shown
from its record in [`results/gallery/`](../results/gallery), including SmolLM2's 34 questions
over 14.8 billion instructions. And the page's referee rules on any record in about a
millisecond, gallery ones included: it needs the witness and proofs, not the run.

## Act 3: X-ray

`rvgo/internal/xray` wraps each step: it reads the PC and instruction word,
decodes the word once per PC (`internal/rvdis`, which agrees with GNU objdump on every
instruction of the embedded guests), and keeps a shadow call stack. A `jal`/`jalr` that
links through `ra` or `t0` pushes a frame, `jalr zero,0(ra)` pops one, and a jump into
another function replaces the top frame. Profiling does not change execution; the
poststate is identical with and without it. Rust symbols are demangled
(`gemm_f32::microkernel::scalar::f32::x2x4`); where a label has to be short, paths inside
`<...>` keep their last segment and long paths lose their front.

```bash
mlvm xray                                  # tables in the terminal
mlvm xray --svg flame.svg                  # flame graph, colored by float share
mlvm xray --folded out.folded              # for flamegraph.pl, inferno or speedscope
mlvm xray --json                           # the full report
mlvm xray --elf ./prog --load-file data.bin    # any RV64IMAFD static ELF
```

The decoder marks its model stages `noinline` so they show up by name; at `-O2` GCC
otherwise inlines the whole forward pass into `main`. The arithmetic is unchanged: seven
prompts produce byte-identical tokens and output before and after.

## Building and serving the playground

```bash
make web                                   # web/dist: mlvm.wasm, page, local determinism.json
python3 -m http.server -d web/dist 8000
node web/test/protocol.mjs web/dist/mlvm.wasm "$(go env GOROOT)/lib/wasm/wasm_exec.js"
```

`protocol.mjs` plays a full dispute across three WebAssembly instances in Node worker
threads and fails if bisection does not land on the lie; CI runs it on every push.

The browser build (`rvgo/wasm`) is about 10 MB, or 4 MB gzipped, and includes
the embedded model. It omits the EVM, so integer verdicts in the browser use `slow.Step`
only. Measured on an Apple M3 Max: Act 1 takes about 8 s in Chrome (5.3 M instructions/s, with
profiling), a lying run about 9 s, and bisection about 5 s with both parties answering in
parallel workers. Each party keeps up to eleven machine copies, about 90 MB. On small
screens or devices that report 4 GB of memory or less, the page saves half as many.
