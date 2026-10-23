#!/usr/bin/env python3
"""dispute-model.py - where should an interactive fraud proof stop bisecting and switch to
a ZK proof?

    ./scripts/dispute-model.py                       # the default sweep
    ./scripts/dispute-model.py --steps 6107466077    # a specific trace
    ./scripts/dispute-model.py --markdown            # the table for docs/

A pure interactive game bisects all the way down to one instruction. That takes
ceil(log2(N)) on-chain rounds, and the cost that dominates a dispute is not gas - it is the
challenge period each round must allow for, so that an honest party who is temporarily
censored still gets to respond. At an hour per round, a 6.1-billion-step trace takes 33
rounds and more than a day; at the multi-hour periods real rollups use, it is a week.

The hybrid alternative: bisect for K rounds, then prove the remaining N/2^K steps in one
ZK proof. Fewer rounds, but the proof gets more expensive the earlier you stop.

This models both and finds the crossover. Every VM-side number is measured (see
docs/BENCHMARKS.md); the ZK numbers are parameters, because they depend on which prover you
pick and they move fast. Defaults are deliberately conservative.
"""

import argparse
import math
import sys

# ---------------------------------------------------------------- measured constants
#
# These come from this repository and are reproducible with `make bench` / `make test`.

GAS_PER_STEP_ONCHAIN = 83_998      # measured: average over 84 conformance suites
GAS_PER_STEP_WORST = 184_757       # measured: worst case over the same suites
GAS_SOFTFLOAT_ADD = 1_756          # measured: rvsol/test/SoftFloat64.t.sol
WITNESS_BYTES = 2_594              # 674-byte state + one 1920-byte merkle branch
EMULATION_STEPS_PER_SEC = 20e6     # measured: ~20 M instr/s on an M3 Max

BERT_BASE_STEPS = 6_107_466_077    # measured: bert-base-uncased, one inference


def bisection_rounds(steps: int) -> int:
    """Rounds needed to isolate a single instruction."""
    return max(1, math.ceil(math.log2(max(steps, 2))))


def model(steps: int, challenge_period_h: float, zk_cycles_per_sec: float,
          zk_verify_gas: int, zk_overhead: float, gas_price_gwei: float,
          eth_usd: float, prover_usd_per_hour: float):
    """Cost and latency for every possible switch point."""
    full_rounds = bisection_rounds(steps)
    rows = []
    for k in range(0, full_rounds + 1):
        remaining = max(1, math.ceil(steps / (2 ** k)))

        # Interactive part: k rounds, each an on-chain transaction plus a challenge period.
        # Both parties post, so two transactions per round.
        interactive_gas = k * 2 * 60_000        # a bisection move is a small state update
        interactive_hours = k * challenge_period_h

        if k == full_rounds:
            # Pure interactive: the last step is executed on chain directly.
            settle_gas = GAS_PER_STEP_ONCHAIN
            prove_hours = 0.0
            mode = "on-chain step"
        else:
            # Hybrid: ZK-prove the remaining segment.
            #
            # zk_overhead is how many zkVM cycles it costs to emulate one MLVM step. A step
            # is a fetch, a decode, a merkle-branch check and the instruction itself; the
            # merkle check dominates and is the reason this is in the hundreds rather than
            # the tens.
            cycles = remaining * zk_overhead
            prove_hours = cycles / zk_cycles_per_sec / 3600
            settle_gas = zk_verify_gas
            mode = "ZK segment"

        total_gas = interactive_gas + settle_gas
        gas_usd = total_gas * gas_price_gwei * 1e-9 * eth_usd
        prove_usd = prove_hours * prover_usd_per_hour
        # One challenge period still has to elapse after the final move.
        total_hours = interactive_hours + challenge_period_h + prove_hours

        rows.append({
            "k": k, "remaining": remaining, "mode": mode,
            "gas": total_gas, "prove_hours": prove_hours,
            "total_hours": total_hours,
            "usd": gas_usd + prove_usd,
        })
    return rows


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--steps", type=int, default=BERT_BASE_STEPS,
                    help="trace length (default: measured bert-base inference)")
    ap.add_argument("--challenge-period", type=float, default=1.0,
                    help="hours per bisection round (default 1; real rollups use more)")
    ap.add_argument("--zk-cycles-per-sec", type=float, default=1e6,
                    help="prover throughput in zkVM cycles/second (default 1M, CPU-class)")
    ap.add_argument("--zk-verify-gas", type=int, default=300_000,
                    help="on-chain gas to verify one proof (default 300k, Groth16-class)")
    ap.add_argument("--zk-overhead", type=float, default=500.0,
                    help="zkVM cycles to emulate one MLVM step (default 500)")
    ap.add_argument("--gas-price", type=float, default=10.0, help="gwei")
    ap.add_argument("--eth-usd", type=float, default=3000.0)
    ap.add_argument("--prover-usd-per-hour", type=float, default=2.0)
    ap.add_argument("--markdown", action="store_true")
    args = ap.parse_args()

    rows = model(args.steps, args.challenge_period, args.zk_cycles_per_sec,
                 args.zk_verify_gas, args.zk_overhead, args.gas_price,
                 args.eth_usd, args.prover_usd_per_hour)

    full = rows[-1]
    # The best hybrid is the one with the lowest wall-clock, since latency is what makes
    # disputes painful; cost is reported alongside so a bad trade is visible.
    hybrids = [r for r in rows if r["mode"] == "ZK segment"]
    best = min(hybrids, key=lambda r: r["total_hours"]) if hybrids else full

    if args.markdown:
        print(f"Trace: {args.steps:,} steps · challenge period {args.challenge_period} h · "
              f"prover {args.zk_cycles_per_sec/1e6:.0f}M cycles/s · "
              f"{args.zk_overhead:.0f} cycles per emulated step\n")
        print("| Bisection rounds | Steps left | Settled by | Gas | Proving | Total latency |")
        print("| ---: | ---: | --- | ---: | ---: | ---: |")
        for r in rows:
            if r["k"] % max(1, len(rows) // 12) and r is not full and r is not best:
                continue
            mark = " **← best**" if r is best else (" (pure interactive)" if r is full else "")
            prove = f"{r['prove_hours']:.2f} h" if r["prove_hours"] >= 0.01 else "—"
            print(f"| {r['k']}{mark} | {r['remaining']:,} | {r['mode']} | "
                  f"{r['gas']:,} | {prove} | {r['total_hours']:.1f} h |")
        return 0

    print(f"trace                {args.steps:,} steps")
    print(f"challenge period     {args.challenge_period} h per round")
    print(f"prover               {args.zk_cycles_per_sec/1e6:.0f}M cycles/s, "
          f"{args.zk_overhead:.0f} cycles per emulated step")
    print(f"verify               {args.zk_verify_gas:,} gas")
    print()
    print(f"{'rounds':>7} {'steps left':>14} {'settled by':>14} {'gas':>10} "
          f"{'prove':>10} {'latency':>10} {'cost':>9}")
    print("-" * 80)
    for r in rows:
        if r["k"] % max(1, len(rows) // 16) and r is not full and r is not best:
            continue
        tag = " <- best" if r is best else ""
        prove = f"{r['prove_hours']:.2f}h" if r["prove_hours"] >= 0.01 else "-"
        print(f"{r['k']:>7} {r['remaining']:>14,} {r['mode']:>14} {r['gas']:>10,} "
              f"{prove:>10} {r['total_hours']:>9.1f}h ${r['usd']:>8.2f}{tag}")

    print()
    print(f"pure interactive : {full['k']} rounds, {full['total_hours']:.1f} h, "
          f"{full['gas']:,} gas")
    print(f"best hybrid      : {best['k']} rounds, {best['total_hours']:.1f} h, "
          f"{best['gas']:,} gas, proving {best['prove_hours']:.2f} h")
    if full["total_hours"] > 0:
        print(f"latency reduction: {full['total_hours'] / best['total_hours']:.1f}x")
    print()
    print("The hybrid wins on latency, not on gas: a ZK verification costs several times an")
    print("on-chain step, but it removes most of the challenge periods, and those dominate.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
