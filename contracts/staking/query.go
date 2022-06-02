package staking

import (
	"errors"
	"math/big"
	"strings"

	"github.com/TIE-Tech/tie-core/contracts/abis"
	"github.com/TIE-Tech/tie-core/tievm/evm"
	"github.com/TIE-Tech/tie-core/types"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/umbracle/ethgo"
	"github.com/umbracle/ethgo/abi"
)

var (
	// staking contract address
	AddrStakingContract = types.StringToAddress("1001")

	// Gas limit used when querying the validator set
	queryGasLimit uint64 = 100000
)

type TxQueryHandler interface {
	Apply(*types.Transaction) (*evm.ExecutionResult, error)
	GetNonce(types.Address) uint64
}

// QueryValidators
func QueryValidators(t TxQueryHandler, from types.Address) ([]types.Address, error) {
	method, ok := abis.StakingABI.Methods["validators"]
	if !ok {
		return nil, errors.New("validators method doesn't exist in Staking contract ABI")
	}

	selector := method.ID()
	res, err := t.Apply(&types.Transaction{
		From:     from,
		To:       &AddrStakingContract,
		Value:    big.NewInt(0),
		Input:    selector,
		GasPrice: big.NewInt(0),
		Gas:      queryGasLimit,
		Nonce:    t.GetNonce(from),
	})
	if err != nil {
		return nil, err
	}

	if res.Failed() {
		return nil, res.Err
	}
	return DecodeValidators(method, res.ReturnValue)
}

// DecodeValidators
func DecodeValidators(method *abi.Method, returnValue []byte) ([]types.Address, error) {
	decodedResults, err := method.Outputs.Decode(returnValue)
	if err != nil {
		return nil, err
	}

	results, ok := decodedResults.(map[string]interface{})
	if !ok {
		return nil, errors.New("failed type assertion from decodedResults to map")
	}

	web3Addresses, ok := results["0"].([]ethgo.Address)
	if !ok {
		return nil, errors.New("failed type assertion from results[0] to []web3.Address")
	}

	addresses := make([]types.Address, len(web3Addresses))
	for idx, waddr := range web3Addresses {
		addresses[idx] = types.Address(waddr)
	}
	return addresses, nil
}

// QueryAccountStake
func QueryAccountStake(t TxQueryHandler, from types.Address) (amount *big.Int, err error) {

	method, ok := abis.StakingABI.Methods["accountStake"]
	if !ok {
		return amount, errors.New("validators method doesn't exist in Staking contract ABI")
	}

	parsed, err := ethabi.JSON(strings.NewReader(abis.StakingJSONABI))
	if err != nil {
		return amount, err
	}

	input, err := parsed.Pack("accountStake", from)
	if err != nil {
		return amount, err
	}

	res, err := t.Apply(&types.Transaction{
		From:     from,
		To:       &AddrStakingContract,
		Value:    big.NewInt(0),
		Input:    input,
		GasPrice: big.NewInt(0),
		Gas:      queryGasLimit,
		Nonce:    t.GetNonce(from),
	})
	if err != nil {
		return amount, err
	}

	if res.Failed() {
		return amount, res.Err
	}
	return DecodeAccountStake(method, res.ReturnValue)
}

// DecodeAccountStake
func DecodeAccountStake(method *abi.Method, returnValue []byte) (value *big.Int, err error) {
	decodedResults, err := method.Outputs.Decode(returnValue)
	if err != nil {
		return value, err
	}

	results, ok := decodedResults.(map[string]interface{})
	if !ok {
		return value, errors.New("failed type assertion from decodedResults to map")
	}

	amount, ok := results["0"].(*big.Int)
	if !ok {
		return value, errors.New("failed type assertion from results[0] to bigint")
	}
	return amount, nil
}
