package liar

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/yao-jz/mlvm/rvgo/fast"
)

// RunUntilDisputed runs a dishonest party whose lie must leave a trace. A lie that
// outlives its lockstep window can still vanish later, leaving a final state equal to the
// honest one (honestPost), and then there is nothing to dispute. A kept integer lie can
// also make the program fault, or run past fault.StepLimit, later on. In either case the liar starts over and lies at
// the next candidate after that one, up to tries runs.
// setup, if set, configures each new party (stdout, progress) before it runs.
func RunUntilDisputed(name string, pre *fast.VMState, fault Fault, interval uint64, honestPost common.Hash, tries int, setup func(*Party)) (*Party, error) {
	var prior []Attempt
	for run := 1; ; run++ {
		f := fault
		p := NewParty(name, pre, &f, interval)
		p.prior = prior
		if setup != nil {
			setup(p)
		}
		err := p.Run(fault.StepLimit, nil)
		dev := p.Deviation()
		retry := fault.Kind.lockstep() && dev != nil && run < tries
		if err != nil && !retry {
			return nil, err
		}
		if err == nil && (!retry || p.FinalHash() != honestPost) {
			return p, nil
		}
		prior = p.Attempts()
		for i := range prior {
			if prior[i].Kept && prior[i].Step == dev.Step {
				prior[i].Kept = false
				if err != nil {
					prior[i].Faulted = true // the program faulted later on
				} else {
					prior[i].Vanished = true
				}
			}
		}
		fault.AtStep = dev.Step + 1
	}
}
