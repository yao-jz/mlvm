# Dispute Game v0 (Experimental)

Format ID: `mlvm.dispute/0`

This is **not** a production fault-dispute game. It provides:

1. Trace claim over `(prestate, poststate, stepCount)`
2. Midpoint bisection over instruction indices
3. Terminal routes:
   - direct `RISCV.step` for integer/memory transitions
   - intended: a bound nested co-processor for arithmetic float transitions
4. Final verdict readable by a local demo / embedded EVM

Current implementation: `MLDisputeGame.sol` exposes `initialize`, `bisect`, `stepDirect`
(a terminal `RISCV.step`) and `readAnswer`. It stores a `FloatSubGame` address but has no
float terminal function, and no nested `f32` co-processor exists; the experimental `f64`
sub-game is round-to-nearest-even only and is not bound to an outer instruction on chain.
Arithmetic float transitions therefore have no terminal route in this game.

Intentionally absent: bonds, timeouts, challenger services, deployment addresses, audits.
