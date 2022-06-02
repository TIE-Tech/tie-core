package execute

import (
	"errors"
	"github.com/tie-core/params"
	"github.com/tie-core/tievm/evm"
)

var _ evm.Runtime = &EVM{}

// EVM is the ethereum virtual machine
type EVM struct {
}

// NewEVM creates a new EVM
func NewEVM() *EVM {
	return &EVM{}
}

// CanRun implements the runtime interface
func (e *EVM) CanRun(*evm.Contract, evm.Host, *params.ForksInTime) bool {
	return true
}

// Name implements the runtime interface
func (e *EVM) Name() string {
	return "evm"
}

// Run implements the runtime interface
func (e *EVM) Run(c *evm.Contract, host evm.Host, config *params.ForksInTime) *evm.ExecutionResult {
	contract := acquireState()
	contract.resetReturnData()

	contract.msg = c
	contract.code = c.Code
	contract.evm = e
	contract.gas = c.Gas
	contract.host = host
	contract.config = config

	contract.bitmap.setCode(c.Code)

	ret, err := contract.Run()

	// We are probably doing this append magic to make sure that the slice doesn't have more capacity than it needs
	var returnValue []byte
	returnValue = append(returnValue[:0], ret...)

	gasLeft := contract.gas

	releaseState(contract)

	if err != nil && !errors.Is(err, errRevert) {
		gasLeft = 0
	}

	return &evm.ExecutionResult{
		ReturnValue: returnValue,
		GasLeft:     gasLeft,
		Err:         err,
	}
}