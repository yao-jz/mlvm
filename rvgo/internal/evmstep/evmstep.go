//go:build !js

// Package evmstep runs one MLVM step through RISCV.sol's compiled bytecode in an
// in-process EVM, so a tool can show what the on-chain step function itself returns for a
// transition, and how much gas it used.
//
// The bytecode is embedded from rvsol/out (solc 0.8.15, london); `make
// evm-artifact` refreshes it after RISCV.sol changes, and TestEmbeddedMatchesArtifact
// fails when the two drift apart. Nothing here deploys to a chain.
package evmstep

import (
	_ "embed"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

//go:embed RISCV.deployed.hex
var deployedHex string

// Available reports whether this build can execute RISCV.sol (false in the browser build).
const Available = true

var (
	riscvAddr  = common.HexToAddress("0x1337")
	oracleAddr = common.HexToAddress("0xf00d")
	sender     = common.HexToAddress("0x7070")
)

// Code returns the embedded RISCV.sol runtime bytecode.
func Code() ([]byte, error) {
	return hex.DecodeString(strings.TrimSpace(deployedHex))
}

// Step calls RISCV.sol's step(bytes,bytes,bytes32) with an ABI-encoded step input (see
// fast.StepWitness.EncodeStepInput) and returns the post-state hash it computes and the
// gas it used. Steps that read the pre-image oracle are not supported.
func Step(input []byte) (post common.Hash, gasUsed uint64, err error) {
	code, err := Code()
	if err != nil {
		return common.Hash{}, 0, fmt.Errorf("embedded RISCV.sol bytecode: %w", err)
	}
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	if err != nil {
		return common.Hash{}, 0, err
	}
	statedb.SetCode(riscvAddr, code)
	// Storage slot 0 holds the pre-image oracle address, as in the contract's constructor.
	statedb.SetState(riscvAddr, common.Hash{}, common.BytesToHash(oracleAddr.Bytes()))

	cfg := params.MainnetChainConfig
	blockCtx := vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		Coinbase:    common.HexToAddress("0xdeadbeef"),
		GasLimit:    30_000_000,
		BlockNumber: big.NewInt(100),
		Time:        1000,
		Difficulty:  big.NewInt(0),
		BaseFee:     big.NewInt(0),
		// A post-merge block context selects the merge instruction set (London opcodes,
		// no PUSH0), which matches the contract's solc 0.8.15 / london build.
		Random: &common.Hash{},
	}
	env := vm.NewEVM(blockCtx, vm.TxContext{}, statedb, cfg, vm.Config{})
	rules := cfg.Rules(blockCtx.BlockNumber, true, blockCtx.Time)
	statedb.Prepare(rules, sender, blockCtx.Coinbase, &riscvAddr, vm.ActivePrecompiles(rules), nil)

	const gas = uint64(30_000_000)
	ret, left, err := env.Call(vm.AccountRef(sender), riscvAddr, input, gas, big.NewInt(0))
	if err != nil {
		return common.Hash{}, gas - left, fmt.Errorf("RISCV.sol step reverted: %w (return 0x%x)", err, ret)
	}
	if len(ret) != 32 {
		return common.Hash{}, gas - left, fmt.Errorf("RISCV.sol step returned %d bytes", len(ret))
	}
	return common.BytesToHash(ret), gas - left, nil
}
