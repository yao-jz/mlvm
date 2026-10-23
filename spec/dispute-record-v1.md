# Dispute Record v1 (Experimental)

Format ID: `mlvm.dispute-record/1`

The record of one finished Catch the Liar dispute, written by `mlvm liar --json` and
downloaded from the playground. `mlvm referee` and the playground's referee rule on it
again from the record alone, without running either machine. See
[docs/PLAYGROUND.md](../docs/PLAYGROUND.md).

It is not [`mlvm.dispute/0`](dispute-v0.md), the experimental on-chain game: there is no
contract, bond or clock here, only the transcript of a local bisection and the evidence for
its last step.

## Fields

A JSON object. Hashes are `0x`-prefixed hex; step counts are JSON numbers.

| Field | Meaning |
| --- | --- |
| `format` | `"mlvm.dispute-record/1"` |
| `guest`, `prompt`, `lie` | What ran and how the liar lied. Descriptive; the referee does not use them. |
| `honestText`, `liarText`, `honestCommitted`, `liarCommitted` | Each machine's answer, printed and committed. Descriptive. |
| `attempts`, `deviation` | The liar's own account of its lies. Descriptive. |
| `honestSteps`, `liarSteps` | Instructions each machine executed before halting. |
| `prestate` | The state hash both machines start from. |
| `honestPost`, `liarPost` | Each machine's final state hash. |
| `rounds[]` | The bisection: `n`, `lo`, `hi`, `mid`, the `honest` and `liar` answers at `mid`, and `agree`. |
| `verdict` | The disputed instruction and the evidence for it (below). |
| `noDispute` | Set when both machines end in the same state; there is then no verdict. |

`verdict` holds `step`, `pc`, `word` (the instruction word, hex), `instruction`
(disassembly), `function`, `class`, `agreedPrestate`, `honestPost`, `liarPost`,
`refereePost`, `winner`, `method`, `stateWitness` (the 674-byte
[state witness](state-witness-v1.md) at `step`), `memProof` (the memory Merkle proofs the
instruction consumes, 1,920 bytes each, instruction fetch first), `fsProof` (the
[file-system proof](filesystem-proof-v2.md), when the instruction reads file metadata),
`proofBytes`, `boundary`, and, when RISCV.sol ruled, `evm` (`post`, `gasUsed`,
`matchesSlowStep`).

## Checking a record

A referee who saw none of the dispute accepts a record only if all of these hold:

1. With `lo = 0`, `hi = max(honestSteps, liarSteps)`, each round `i` has `n = i + 1`, the
   current `lo` and `hi`, `hi - lo > 1` and `mid = lo + floor((hi - lo) / 2)`, and `agree`
   is true exactly when the two answers are equal. An agreeing round sets `lo = mid`, a
   differing one `hi = mid`. After the last round `hi - lo = 1`.
2. `verdict.step = lo`. `verdict.agreedPrestate` is the answer both gave at `lo` (or
   `prestate` when `lo = 0`), and `verdict.honestPost` and `verdict.liarPost` are their
   answers at `hi` (or `honestPost` and `liarPost` when `hi` was never asked).
3. `stateWitness` hashes to `agreedPrestate`, and its step field is `lo`.
4. The first memory proof proves the instruction word at the witness's `pc` against the
   witness's memory root, and it equals `verdict.word`.
5. Executing that one instruction gives `refereePost`: floating-point arithmetic is
   re-executed with SoftFloat from the witness registers and `fcsr`; every other
   instruction goes through `slow.Step` (and, in the native CLI, RISCV.sol's compiled step
   function in an EVM, which must agree) with the witness and proofs as its input.

The winner is the party whose answer at `hi` equals `refereePost`. `mlvm referee` also
fails when the record's own `refereePost` or `winner` differs from its ruling.

The function name in `verdict.function` is not checked: that needs the guest's ELF.
