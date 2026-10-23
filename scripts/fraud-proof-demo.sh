#!/usr/bin/env bash
#
# fraud-proof-demo.sh - walk the single-step witness pipeline on a real program.
#
#   ./scripts/fraud-proof-demo.sh [elf] [step]
#
# In a complete interactive fraud-proof protocol, two parties run the same program and publish
# a commitment to the final state. If they disagree, they bisect the execution trace until
# they are down to a single instruction that one says produces state A and the other says
# produces state B. A terminal verifier checks that one step, and the surrounding protocol
# applies its own bond and resolution rules.
#
# This script walks the local machinery that a dispute protocol would need at its terminal
# step. It does not run two parties, an interactive game, bonds, or timeouts:
#
#   1. load the program into a deterministic initial state
#   2. run it to completion and commit to the result
#   3. replay to a selected candidate step and write representative checkpoints
#   4. emit the proof for that one step - a 674-byte state witness plus the merkle
#      branches for exactly the memory the instruction touches
#   5. flip a single bit of the committed state and watch the commitment change; a complete
#      protocol would use that commitment sensitivity to detect a conflicting claim
#
# Defaults to examples/build/hello, disputing the step half way through its trace.
# Pass an ELF and a step number to use your own.

set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

ELF="${1:-examples/build/hello}"
STEP="${2:-}"   # default: half way through the trace, discovered below
WORK="$REPO_ROOT/.mlvm-proof"

require_elf "$ELF"
ensure_mlvm
name="$(basename "$ELF")"

rm -rf "$WORK"; mkdir -p "$WORK/proofs" "$WORK/snapshots"

hr() { printf '%s\n' "-------------------------------------------------------------------"; }

echo
hr
echo " STEP 1  Load the program into a deterministic initial state"
hr
"$MLVM_BIN" load-elf \
    --path "$ELF" \
    --out "$WORK/$name.state.json" \
    --meta "$WORK/$name.meta.json" \
    --random-file "$RANDOM_FILE" >"$WORK/load.log" 2>&1
grep -E 'program entry|prog [0-9]+:' "$WORK/load.log" | head -6 | sed 's/^/  /'
python3 - "$WORK/$name.state.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print(f"  initial PC        {d['pc']:#x}")
print(f"  pages mapped      {len(d['memory']):,}  ({len(d['memory'])*4096/1024/1024:.1f} MiB)")
print(f"  stack pointer     {d['registers'][2]:#x}")
print(f"  heap base         {d['heap']:#x}")
PY

echo
hr
echo " STEP 2  Run the program to completion and commit to the result"
hr
"$MLVM_BIN" run \
    --input "$WORK/$name.state.json" \
    --output "$WORK/$name.final.json" \
    --meta "$WORK/$name.meta.json" \
    --info-at '%100000000' >"$WORK/run.log" 2>&1
grep -E '  (steps|integer ALU|memory load|float ALU|pages touched|throughput)' "$WORK/run.log" | sed 's/^/  /' || true

TOTAL=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['step'])" "$WORK/$name.final.json")
if [[ -z "$STEP" ]]; then
    STEP=$((TOTAL / 2))
fi
if (( STEP >= TOTAL )); then
    echo "  requested step $STEP is past the end of the trace ($TOTAL steps)" >&2
    exit 1
fi
echo "  total trace length  $TOTAL steps"
echo "  candidate step      $STEP  (selected locally; no interactive game is running)"

echo
hr
echo " STEP 3  Replay to the candidate step, checkpointing on the way"
hr
INTERVAL=$(( STEP / 4 )); (( INTERVAL > 0 )) || INTERVAL=1
"$MLVM_BIN" run \
    --input "$WORK/$name.state.json" \
    --output "$WORK/$name.mid.json" \
    --meta "$WORK/$name.meta.json" \
    --stop-at "=$STEP" \
    --snapshot-at "%$INTERVAL" \
    --snapshot-fmt "$WORK/snapshots/snap-%d.json" \
    --info-at '%100000000' >>"$WORK/run.log" 2>&1
echo "  representative checkpoints written:"
ls "$WORK/snapshots" | sed 's/^/    /'

echo
hr
echo " STEP 4  Prove the single disputed instruction"
hr
"$MLVM_BIN" run \
    --input "$WORK/$name.mid.json" \
    --output "$WORK/$name.post.json" \
    --meta "$WORK/$name.meta.json" \
    --proof-at "=$STEP" \
    --proof-fmt "$WORK/proofs/proof-%d.json" \
    --stop-at "=$((STEP + 1))" >>"$WORK/run.log" 2>&1

PROOF="$WORK/proofs/proof-$STEP.json"
if [[ ! -f "$PROOF" ]]; then
    echo "  no proof emitted at step $STEP (did the program exit earlier?)" >&2
    tail -5 "$WORK/run.log" >&2
    exit 1
fi

python3 - "$PROOF" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
state = bytes.fromhex(d["state-data"][2:])
proof = bytes.fromhex(d["proof-data"][2:])
print(f"  disputed step     {d['step']:,}")
print(f"  pre-state  hash   {d['pre']}")
print(f"  post-state hash   {d['post']}")
print()
print(f"  state witness     {len(state)} bytes")
print(f"    memory root       {state[0:32].hex()}")
print(f"    preimage key      {state[32:64].hex()}")
print(f"    pc                {int.from_bytes(state[72:80],'big'):#x}")
print(f"    exit / exited     {state[80]} / {state[81]}")
print(f"    step              {int.from_bytes(state[82:90],'big'):,}")
print(f"    heap              {int.from_bytes(state[90:98],'big'):#x}")
print(f"    32 x integer reg  {8*32} bytes")
print(f"    32 x float reg    {8*32} bytes")
print()
print(f"  memory proof      {len(proof)} bytes = {len(proof)//32} merkle nodes")
print(f"                    ({len(proof)//32//60} branch(es) x 60 levels, one per 32-byte")
print(f"                     word the instruction touched)")
print()
print("  These are the state and memory inputs for a supported single-step verifier.")
print("  A complete protocol must still bind this step to trace commitments and enforce")
print("  the game, timeout, and claim rules described in docs/STATUS.md.")
PY

echo
hr
echo " STEP 5  Commitment sensitivity: one changed bit moves the hash"
hr
python3 - "$PROOF" <<'PY'
import json, sys

# Keccak-256 is what the VM uses. Note that the VM overwrites the first byte of the hash
# with a status code (0 valid, 1 invalid, 2 panic, 3 unfinished), which is why the `pre`
# field above starts with 0x03 while the raw keccak below starts with something else.
# Fall back to a clear message if no keccak implementation is installed.
try:
    from Crypto.Hash import keccak
    def k256(b):
        h = keccak.new(digest_bits=256); h.update(b); return h.digest()
except Exception:
    try:
        import sha3
        def k256(b): return sha3.keccak_256(b).digest()
    except Exception:
        k256 = None

d = json.load(open(sys.argv[1]))
state = bytearray(bytes.fromhex(d["state-data"][2:]))

if k256 is None:
    print("  (install pycryptodome or pysha3 to see the recomputed hashes)")
    print(f"  committed post-state hash : {d['post']}")
    print("  Flipping any single bit of the 674-byte witness - one register, one byte of")
    print("  the memory root, the step counter - changes that hash completely. Detecting")
    print("  and penalising a false claim still requires an outer dispute protocol.")
else:
    orig = k256(bytes(state))
    print(f"  keccak(state)             0x{orig.hex()}")
    # Flip the low bit of register a0 (x10): offset 32+32+8+8+1+1+8+8+8 = 106, + 10*8
    off = 106 + 10 * 8 + 7
    state[off] ^= 0x01
    tampered = k256(bytes(state))
    print(f"  keccak(state, a0 ^= 1)    0x{tampered.hex()}")
    diff = sum(bin(a ^ b).count("1") for a, b in zip(orig, tampered))
    print(f"  bits changed in the hash  {diff} of 256")
    print()
    print("  One bit of one register changes the commitment. An outer protocol can use this")
    print("  property to bind checkpoints, but that protocol is not implemented here.")
PY

echo
hr
echo " Artifacts kept in .mlvm-proof/ (proofs/, snapshots/, *.json)"
hr
