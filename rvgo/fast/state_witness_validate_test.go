package fast

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStateWitnessValidateExactLength(t *testing.T) {
	st := NewVMState()
	w := st.EncodeWitness()
	require.NoError(t, w.Validate())
	_, err := StateWitness(w[:len(w)-1]).StateHash()
	require.Error(t, err)
	bad := append(StateWitness(nil), w...)
	bad = append(bad, 0)
	_, err = StateWitness(bad).StateHash()
	require.Error(t, err)
}

func TestStateWitnessRejectsNonCanonicalExited(t *testing.T) {
	st := NewVMState()
	w := st.EncodeWitness()
	w[81] = 2
	require.Error(t, StateWitness(w).Validate())
}
