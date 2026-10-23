//go:build js

package evmstep

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
)

// Available is false in the browser build, which does not carry an EVM.
const Available = false

// Step is not available in the browser build.
func Step([]byte) (common.Hash, uint64, error) {
	return common.Hash{}, 0, errors.New("RISCV.sol execution is not included in the browser build")
}
