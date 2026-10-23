#!/usr/bin/env bash
#
# ml-claim.sh - turn a neural network's run into a claim a contract can check.
#
#   ./scripts/ml-claim.sh
#
# A fault proof settles "this machine went from state A to state B". That is not yet a claim
# about a model: nothing binds B to "the answer was Y", and a guest's stdout is not part of
# the state commitment, so a dishonest party could print anything.
#
# The fix is that the guest writes its answer into its own memory at a fixed address, so the
# final memory root commits to it and a contract reads it back with one merkle branch - the
# same trick the float sub-game uses to read a result out of a nested machine, one level up.
#
# Four acts:
#
#   1  the prestate commits to the program and its input, reproducibly
#   2  the network runs and commits its answer to memory rather than to stdout
#   3  MLClaim.sol reads that answer out of the committed state, in a real EVM
#   4  flipping one bit of the answer moves the state hash, so it cannot be forged
#
# What this does NOT do is settle a dispute - that needs the game contract and a challenger.
# This is the piece that makes the game about an inference rather than about an anonymous
# state transition. See docs/ARCHITECTURE.md#claiming-an-inference.

set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

if [[ ! -f examples/build/mlp ]]; then
    echo "==> building the guest program"
    make -s examples
fi

if [[ ! -f rvsol/out/MLClaim.sol/MLClaim.json ]]; then
    echo "==> building contracts"
    (cd rvsol && forge build) >/dev/null
fi

echo
# -v so the narrative reaches stdout. The test prints it directly rather than through t.Logf,
# whose file:line prefix would sit on every line.
(go test ./rvgo/test/ -run TestMLClaimReadback -count=1 -v -timeout 10m) \
    | grep -v -E '^(=== RUN|--- PASS|--- SKIP|PASS$|ok  )'

cat <<'EOF'

==> the claim, from the command line

    bin/mlvm ml-claim --path examples/build/mlp --prestate     # just the commitment
    bin/mlvm ml-claim --path examples/build/mlp                # the whole claim
EOF
