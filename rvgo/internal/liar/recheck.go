package liar

import (
	"encoding/binary"
	"fmt"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

// RecordFormat names the record of a finished dispute: the JSON that `mlvm liar --json`
// prints and the playground downloads (spec/dispute-record-v1.md). Recheck rules on it
// again.
const RecordFormat = "mlvm.dispute-record/1"

// Recheck rules on a finished dispute from its record alone, the way a referee who saw
// none of it would: it runs neither machine and trusts neither party's verdict. It checks
// that the questions form a bisection (each asks the midpoint of the interval still in
// dispute, and the interval shrinks to the side where the answers differ) that ends on a
// single instruction; that the state witness hashes to the state both parties agreed on
// before it, at that step; and then executes that one instruction from the witness and its
// Merkle proofs (Referee). The returned verdict names the winner by comparing the result
// with what each party claimed after the instruction.
//
// The function name in the record is not checked: it needs the guest's ELF.
func Recheck(res *Result) (*Verdict, error) {
	if res.NoDispute || res.HonestPost == res.LiarPost {
		return nil, fmt.Errorf("both machines claim the same poststate: there is nothing to dispute")
	}
	rec := res.Verdict
	if rec == nil {
		return nil, fmt.Errorf("the record has no verdict to check")
	}

	// Replay the questions. Invariant, as in Play: the parties agree after lo instructions
	// (on agreed) and disagree after hi (honestAt against liarAt).
	lo, hi := uint64(0), max(res.HonestSteps, res.LiarSteps)
	agreed := res.Prestate
	honestAt, liarAt := res.HonestPost, res.LiarPost
	for i, r := range res.Rounds {
		if hi-lo <= 1 {
			return nil, fmt.Errorf("question %d comes after the dispute was down to one instruction", i+1)
		}
		mid := lo + (hi-lo)/2
		if r.N != i+1 || r.Lo != lo || r.Hi != hi || r.Mid != mid {
			return nil, fmt.Errorf("question %d asks about step %d of [%d, %d]; bisection asks about step %d of [%d, %d]", i+1, r.Mid, r.Lo, r.Hi, mid, lo, hi)
		}
		if r.Agree != (r.Honest == r.Liar) {
			return nil, fmt.Errorf("question %d is marked agree=%v, but the answers are %s and %s", i+1, r.Agree, r.Honest.Hex(), r.Liar.Hex())
		}
		if r.Agree {
			lo, agreed = mid, r.Honest
		} else {
			hi, honestAt, liarAt = mid, r.Honest, r.Liar
		}
	}
	if hi-lo != 1 {
		return nil, fmt.Errorf("the questions stop with %d instructions still in dispute", hi-lo)
	}
	if rec.Step != lo {
		return nil, fmt.Errorf("the verdict is about step %d, but bisection isolated step %d", rec.Step, lo)
	}
	if rec.Agreed != agreed || rec.HonestPost != honestAt || rec.LiarPost != liarAt {
		return nil, fmt.Errorf("the verdict's hashes are not the ones the parties gave during bisection")
	}

	state, err := hexutil.Decode(rec.StateWitness)
	if err != nil {
		return nil, fmt.Errorf("state witness: %w", err)
	}
	if len(state) != fast.StateWitnessSize {
		return nil, fmt.Errorf("state witness is %d bytes, want %d", len(state), fast.StateWitnessSize)
	}
	if at := binary.BigEndian.Uint64(state[witStep:]); at != lo {
		return nil, fmt.Errorf("the state witness is of step %d, not %d", at, lo)
	}
	memProof, err := hexutil.Decode(rec.MemProof)
	if err != nil {
		return nil, fmt.Errorf("memory proof: %w", err)
	}
	var fsProof []byte
	if rec.FSProof != "" {
		if fsProof, err = hexutil.Decode(rec.FSProof); err != nil {
			return nil, fmt.Errorf("file-system proof: %w", err)
		}
	}
	word, err := strconv.ParseUint(rec.Word, 0, 32)
	if err != nil {
		return nil, fmt.Errorf("instruction word %q: %w", rec.Word, err)
	}

	t := &Transition{
		Step: lo, PC: rec.PC, Word: uint32(word), Pre: agreed, Post: honestAt,
		Witness: &fast.StepWitness{State: state, MemProof: memProof, FileSystemProof: fsProof},
	}
	v, err := Referee(t)
	if err != nil {
		return nil, err
	}
	v.HonestPost, v.LiarPost = honestAt, liarAt
	v.Function = rec.Function
	v.Winner = winner(v.RefereePost, honestAt, liarAt)
	return v, nil
}

// winner names the party whose claim matches the referee's result.
func winner(referee, honest, liar common.Hash) string {
	switch referee {
	case honest:
		return "honest"
	case liar:
		return "liar"
	}
	return "neither"
}
