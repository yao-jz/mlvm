// The referee's side of a dispute, run on the main thread: bisect on two machines'
// state hashes, then hand the one disputed instruction to an independent referee.
// It mirrors liar.Play in rvgo/internal/liar/game.go.

// bisect(honest, liar, total, onRound) -> { lo, rounds }
// Invariant: the machines agree on the state after lo instructions and disagree after hi.
export async function bisect(honest, liar, total, onRound) {
  // A dispute needs a shared starting point and different end states.
  const [h0, l0, hEnd, lEnd] = await Promise.all([
    honest.hashAt(0), liar.hashAt(0), honest.hashAt(total), liar.hashAt(total),
  ]);
  if (h0 !== l0) throw new Error("the two RISC-V VMs started with different programs or inputs");
  if (hEnd === lEnd) return { lo: null, rounds: [], noDispute: true };
  let lo = 0;
  let hi = total;
  const rounds = [];
  for (let n = 1; hi - lo > 1; n++) {
    const mid = lo + Math.floor((hi - lo) / 2);
    const [h, l] = await Promise.all([honest.hashAt(mid), liar.hashAt(mid)]);
    const round = { n, lo, hi, mid, honest: h, liar: l, agree: h === l };
    // Moves are public: each machine learns the outcome and keeps its last agreed state.
    await Promise.all([honest.observe(round), liar.observe(round)]);
    rounds.push(round);
    if (onRound) await onRound(round);
    if (round.agree) lo = mid;
    else hi = mid;
  }
  return { lo, rounds };
}

// adjudicate(honest, liar, referee, lo) -> verdict
export async function adjudicate(honest, liar, referee, lo) {
  const [transition, agreed, liarPost] = await Promise.all([
    honest.transitionAt(lo),
    liar.hashAt(lo),
    liar.hashAt(lo + 1),
  ]);
  if (agreed !== transition.pre) {
    throw new Error("the two VM hashes do not match before the disputed RISC-V instruction");
  }
  const verdict = await referee.referee(transition, liarPost);
  return { transition, verdict };
}

// disputeRecord(...) -> the record of a finished dispute in the format `mlvm liar --json`
// writes (mlvm.dispute-record/1, spec/dispute-record-v1.md), so `mlvm referee` and the
// playground's own check can rule on it again.
export function disputeRecord({ guest, prompt, honest, liar, rounds, verdict }) {
  return {
    format: "mlvm.dispute-record/1",
    guest,
    prompt,
    lie: liar.lie,
    honestText: honest.text,
    liarText: liar.text,
    attempts: liar.attempts,
    honestSteps: honest.steps,
    liarSteps: liar.steps,
    prestate: honest.prestate,
    honestPost: honest.poststate,
    liarPost: liar.poststate,
    deviation: liar.deviation,
    rounds,
    verdict,
  };
}
