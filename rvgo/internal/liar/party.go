// Package liar plays a dispute between an honest machine and one that lies about a
// single instruction, and finds the lie by bisection.
//
// Both parties start from the same prestate. The dishonest one deviates at one step,
// either by nudging an f32 arithmetic result by one unit in the last place or by forging
// a byte of the answer it commits. Each party answers only one kind of question: "what is
// your state hash after n instructions?" A referee bisects on the first step at which the
// answers differ, then checks that one instruction from the agreed state witness.
//
// This is a local simulation of the shape of an optimistic dispute. There are no bonds,
// clocks or chain; see docs/STATUS.md for what is and is not settleable on chain.
package liar

import (
	"fmt"
	"io"
	"sort"

	"github.com/ethereum/go-ethereum/common"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

// DefaultCheckpointInterval is how many instructions a party executes between saved
// copies of its machine, at first. Smaller intervals answer queries faster and keep more
// copies. Copies share memory pages until one of them writes (see fast.Memory.Clone).
const DefaultCheckpointInterval = 1 << 22

// MaxCheckpoints bounds the copies a party keeps. When a long run would save more, the
// party doubles its interval and drops every other copy, so a billion-instruction guest
// keeps as many copies as a short one, just further apart.
const MaxCheckpoints = 64

// Party is one side of a dispute: a machine that runs a program once, keeps periodic
// copies of its state, and can then report its state hash, or a step witness, at any
// step by replaying from the nearest copy.
type Party struct {
	Name string

	interval    uint64
	fault       *Fault
	plan        *plan
	checkpoints []*fast.VMState
	// lo is the latest state both parties agreed on; last is the state at the latest
	// question. Observe promotes last to lo when the answers matched.
	lo, last  *fast.VMState
	final     *fast.VMState
	finalHash common.Hash
	stdout    io.Writer
	stepper   func(*fast.InstrumentedState, *fast.VMState) error
	tickEvery uint64
	tick      func(step uint64)
	prior     []Attempt // lies from earlier runs that vanished (RunUntilDisputed)

	// Replayed counts instructions re-executed to answer queries after the first run.
	Replayed uint64
}

// NewParty prepares a party from a prestate, which it copies. fault is nil for the honest
// party. interval 0 selects DefaultCheckpointInterval.
func NewParty(name string, pre *fast.VMState, fault *Fault, interval uint64) *Party {
	if interval == 0 {
		interval = DefaultCheckpointInterval
	}
	return &Party{
		Name:        name,
		interval:    interval,
		fault:       fault,
		checkpoints: []*fast.VMState{pre.Clone()},
		stdout:      io.Discard,
	}
}

// SetStdout receives what the guest prints during the first run.
func (p *Party) SetStdout(w io.Writer) { p.stdout = w }

// SetStepper replaces how the first run executes an instruction, for example with an
// xray.Profiler's Step. Replays always step plainly.
func (p *Party) SetStepper(step func(*fast.InstrumentedState, *fast.VMState) error) { p.stepper = step }

// SetTick calls fn every `every` instructions of the first run.
func (p *Party) SetTick(every uint64, fn func(step uint64)) { p.tickEvery, p.tick = every, fn }

// Run executes the program to completion, saving a copy of the machine every interval
// instructions. A dishonest party chooses and applies its deviation during this run.
// progress, if set, is called every interval with the current step.
func (p *Party) Run(maxSteps uint64, progress func(step uint64)) error {
	state := p.checkpoints[0].Clone()
	inst := fast.NewInstrumentedState(state, nil, p.stdout, io.Discard)
	if p.fault != nil {
		p.plan = &plan{fault: *p.fault}
	}
	for !state.Exited {
		if maxSteps > 0 && state.Step >= maxSteps {
			return fmt.Errorf("%s: guest did not halt within %d steps", p.Name, maxSteps)
		}
		output := false
		if p.plan != nil && !p.plan.done() {
			switch {
			case p.fault.Kind.lockstep():
				if cand := p.plan.candidate(state); cand != nil {
					next, err := p.tryLie(state, cand, progress)
					if err != nil {
						return err
					}
					state = next
					inst = fast.NewInstrumentedState(state, nil, p.stdout, io.Discard)
					continue
				}
			case p.fault.Kind == FaultToken:
				p.plan.inspect(state)
			case p.fault.Kind == FaultCommit:
				p.plan.inspectCommit(state)
			case p.fault.Kind == FaultOutput:
				output = true
			}
		}
		at, pc := state.Step, state.PC
		var word uint32
		if output {
			word = state.Instr()
		}
		if p.stepper != nil {
			if err := p.stepper(inst, state); err != nil {
				return fmt.Errorf("%s: step %d: %w", p.Name, at, err)
			}
			if p.plan != nil {
				p.plan.apply(at, state)
			}
		} else if err := p.step(inst, state); err != nil {
			return err
		}
		if output {
			p.plan.inspectOutput(at, pc, word, state)
		}
		if p.tick != nil && state.Step%p.tickEvery == 0 {
			p.tick(state.Step)
		}
		p.checkpoint(state, progress)
	}
	if p.plan != nil && len(p.plan.actions) == 0 {
		return fmt.Errorf("%s: no instruction matched the requested fault (%s)", p.Name, p.fault)
	}
	p.final = state
	h, err := state.EncodeWitness().StateHash()
	if err != nil {
		return err
	}
	p.finalHash = h
	if progress != nil {
		progress(state.Step)
	}
	return nil
}

func (p *Party) checkpoint(state *fast.VMState, progress func(uint64)) {
	if state.Step%p.interval == 0 && !state.Exited {
		p.checkpoints = append(p.checkpoints, state.Clone())
		if len(p.checkpoints) > MaxCheckpoints {
			p.thin()
		}
		if progress != nil {
			progress(state.Step)
		}
	}
}

// thin doubles the checkpoint interval and drops the copies that are not on it. The
// prestate, at step 0, is always kept.
func (p *Party) thin() {
	p.interval *= 2
	kept := p.checkpoints[:0]
	for _, c := range p.checkpoints {
		if c.Step%p.interval == 0 {
			kept = append(kept, c)
		}
	}
	clear(p.checkpoints[len(kept):])
	p.checkpoints = kept
}

// tryLie forks the machine at a candidate instruction: an honest shadow and a copy that
// flips one bit of the result. Both run in lockstep until the copies are identical again
// (rounding absorbed the lie: abandon it and carry on honestly) or the difference has
// outlived the fault's window (keep it: from here on this party is the liar). A lie that
// makes the copy fault is abandoned too. It returns the machine to continue with.
func (p *Party) tryLie(state *fast.VMState, cand *action, progress func(uint64)) (*fast.VMState, error) {
	shadow := state
	lie := state.Clone()
	shadowInst := fast.NewInstrumentedState(shadow, nil, p.stdout, io.Discard)
	lieInst := fast.NewInstrumentedState(lie, nil, io.Discard, io.Discard)
	var shadowCP, lieCP []*fast.VMState
	dirty := map[uint64]bool{}
	attempt := Attempt{Step: shadow.Step, PC: cand.deviation.PC, Instruction: cand.deviation.Instruction}
	at := shadow.Step
	abandon := func() (*fast.VMState, error) {
		attempt.ErasedAfter = shadow.Step - at
		p.plan.attempts = append(p.plan.attempts, attempt)
		p.checkpoints = append(p.checkpoints, shadowCP...)
		return shadow, nil
	}
	// identical confirms the cheap register-and-dirty-word comparison with the full state
	// hash, which also covers syscall-written memory, files and the randomness cursor.
	identical := func() bool {
		if !converged(shadow, lie, dirty) {
			return false
		}
		a, errA := shadow.EncodeWitness().StateHash()
		b, errB := lie.EncodeWitness().StateHash()
		return errA == nil && errB == nil && a == b
	}

	if _, err := shadowInst.Step(false); err != nil {
		return nil, fmt.Errorf("%s: step %d: %w", p.Name, at, err)
	}
	if _, err := lieInst.Step(false); err != nil {
		return nil, fmt.Errorf("%s: step %d: %w", p.Name, at, err)
	}
	cand.mutate(lie)

	window := p.fault.Window
	if window == 0 {
		window = DefaultLieWindow
	}
	for !shadow.Exited && !lie.Exited && shadow.Step-at <= window {
		if identical() {
			return abandon()
		}
		if shadow.PC != lie.PC {
			break // control flow diverged: the lie has visibly changed the program's path
		}
		word := shadow.Instr()
		var addrs []uint64
		if a, ok := storeAddress(word, shadow); ok {
			addrs = append(addrs, a)
			if b, _ := storeAddress(word, lie); b != a {
				addrs = append(addrs, b)
			}
		}
		step := shadow.Step
		if _, err := shadowInst.Step(false); err != nil {
			return nil, fmt.Errorf("%s: step %d: %w", p.Name, step, err)
		}
		if _, err := lieInst.Step(false); err != nil {
			attempt.Faulted = true
			return abandon()
		}
		trackStore(addrs, shadow, lie, dirty)
		if p.tick != nil && shadow.Step%p.tickEvery == 0 {
			p.tick(shadow.Step)
		}
		if shadow.Step%p.interval == 0 {
			shadowCP = append(shadowCP, shadow.Clone())
			lieCP = append(lieCP, lie.Clone())
			if progress != nil {
				progress(shadow.Step)
			}
		}
	}
	if identical() {
		return abandon()
	}
	attempt.Kept = true
	p.plan.actions = append(p.plan.actions, *cand)
	p.plan.attempts = append(p.plan.attempts, attempt)
	p.checkpoints = append(p.checkpoints, lieCP...)
	return lie, nil
}

// step executes one instruction and, for the dishonest party, applies the planned
// deviation if this is the step it chose.
func (p *Party) step(inst *fast.InstrumentedState, state *fast.VMState) error {
	at := state.Step
	if _, err := inst.Step(false); err != nil {
		return fmt.Errorf("%s: step %d: %w", p.Name, at, err)
	}
	if p.plan != nil {
		p.plan.apply(at, state)
	}
	return nil
}

// Final is the halted machine.
func (p *Party) Final() *fast.VMState { return p.final }

// FinalHash is the claimed poststate.
func (p *Party) FinalHash() common.Hash { return p.finalHash }

// Steps is the number of instructions until the guest halted.
func (p *Party) Steps() uint64 { return p.final.Step }

// Deviation describes what the dishonest party did, or nil for the honest one.
func (p *Party) Deviation() *Deviation {
	if p.plan == nil || len(p.plan.actions) == 0 {
		return nil
	}
	d := *p.plan.actions[0].deviation
	return &d
}

// Attempts lists the lies the dishonest party tried before one stuck (FaultULP).
func (p *Party) Attempts() []Attempt {
	out := append([]Attempt(nil), p.prior...)
	if p.plan != nil {
		out = append(out, p.plan.attempts...)
	}
	return out
}

// stateAt returns a fresh copy of this party's machine after step instructions.
func (p *Party) stateAt(step uint64) (*fast.VMState, error) {
	if p.final == nil {
		return nil, fmt.Errorf("%s has not run yet", p.Name)
	}
	if step >= p.final.Step {
		return p.final.Clone(), nil
	}
	// Start from the latest saved machine at or before step.
	i := sort.Search(len(p.checkpoints), func(i int) bool { return p.checkpoints[i].Step > step }) - 1
	start := p.checkpoints[i]
	for _, s := range []*fast.VMState{p.lo, p.last} {
		if s != nil && s.Step <= step && s.Step > start.Step {
			start = s
		}
	}
	state := start.Clone()
	inst := fast.NewInstrumentedState(state, nil, io.Discard, io.Discard)
	for state.Step < step && !state.Exited {
		if err := p.step(inst, state); err != nil {
			return nil, err
		}
		p.Replayed++
	}
	p.last = state
	return state.Clone(), nil
}

// Observe tells the party how a bisection round ended. The moves of a dispute game are
// public, so each party knows the interval still in dispute; keeping its machine at the
// last agreed step means every later question replays only within that interval.
func (p *Party) Observe(r Round) {
	if r.Agree && p.last != nil && p.last.Step == r.Mid {
		p.lo = p.last
	}
}

// HashAt answers the only question a referee asks during bisection.
func (p *Party) HashAt(step uint64) (common.Hash, error) {
	if p.final != nil && step >= p.final.Step {
		return p.finalHash, nil
	}
	state, err := p.stateAt(step)
	if err != nil {
		return common.Hash{}, err
	}
	return state.EncodeWitness().StateHash()
}

// Transition is one instruction as a party claims it: the witness for the state before
// it, the Merkle proofs it touches, and the state hash after it.
type Transition struct {
	Step    uint64
	PC      uint64
	Word    uint32
	Witness *fast.StepWitness
	Pre     common.Hash
	Post    common.Hash
}

// TransitionAt executes instruction step (from the state after step instructions) with
// proof capture, the way a party defends one step to the referee.
func (p *Party) TransitionAt(step uint64) (*Transition, error) {
	state, err := p.stateAt(step)
	if err != nil {
		return nil, err
	}
	pre, err := state.EncodeWitness().StateHash()
	if err != nil {
		return nil, err
	}
	t := &Transition{Step: step, PC: state.PC, Word: state.Instr(), Pre: pre}
	inst := fast.NewInstrumentedState(state, nil, io.Discard, io.Discard)
	at := state.Step
	wit, err := inst.Step(true)
	if err != nil {
		return nil, fmt.Errorf("%s: proving step %d: %w", p.Name, at, err)
	}
	if p.plan != nil {
		p.plan.apply(at, state)
	}
	t.Witness = wit
	if t.Post, err = state.EncodeWitness().StateHash(); err != nil {
		return nil, err
	}
	return t, nil
}
