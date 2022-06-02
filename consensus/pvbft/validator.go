package pvbft

import (
	"github.com/TIE-Tech/go-logger"
	"github.com/TIE-Tech/tie-core/state"
	"github.com/TIE-Tech/tie-core/types"
	"github.com/shopspring/decimal"
	"math/big"
	"sync"
)

type ValidatorSet struct {
	mu         sync.Mutex
	stakeTotal *big.Int
	validators []types.Address
	randPools  []types.Address
	stakeList  map[types.Address]*big.Int
}

var wei = int64(1e18)

// NewValidatorSet
func NewValidatorSet() *ValidatorSet {
	return &ValidatorSet{
		stakeTotal: big.NewInt(0),
		validators: make([]types.Address, 0),
		randPools:  make([]types.Address, 0),
		stakeList:  make(map[types.Address]*big.Int),
	}
}

// AppendStakeList
func (v *ValidatorSet) AppendStakeList(account types.Address, amount *big.Int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.stakeList[account] = amount
}

// DistributeRewardsByRate Distribute rewards by rate
func (v *ValidatorSet) DistributeRewardsByRate(total *big.Int) {
	v.mu.Lock()
	defer v.mu.Unlock()

	// num = amount * pooLen / stakeTotal
	dcHundred := decimal.NewFromInt(100)
	dcStake := decimal.NewFromBigInt(v.stakeTotal, 0)
	indexList := make(map[types.Address]decimal.Decimal)
	for account, amount := range v.stakeList {
		dcAmount := decimal.NewFromBigInt(amount, 0)
		rate := dcAmount.Mul(dcHundred).Div(dcStake)
		indexList[account] = rate
	}

	vf := state.NewValidatorFee()
	dcFeeTotal := decimal.NewFromBigInt(total, 0)
	for account, rate := range indexList {
		reward := dcFeeTotal.Mul(rate).Div(dcHundred).BigInt()
		vf.SetValidatorFee(account, reward)
		logger.Debug("[BFT] set validator fee", "validator", account, "fee", reward)
	}
}

func (v *ValidatorSet) calcPooLen() uint64 {
	pooLen := 0
	vLen := v.Len()
	bmod := (vLen / 100 * 100)
	if bmod == 0 {
		pooLen = 0
	} else {
		pooLen = vLen - vLen%bmod
	}
	pooLen += 100
	return uint64(pooLen)
}

// CalcProposerPoa calculates the address of the next proposer, from the validator set
func (v *ValidatorSet) CalcProposerPoa(round uint64, lastProposer types.Address) types.Address {
	var seed uint64

	if lastProposer == types.ZeroAddress {
		seed = round
	} else {
		offset := 0
		if indx := v.Index(lastProposer); indx != -1 {
			offset = indx
		}

		seed = uint64(offset) + round + 1
	}

	pick := seed % uint64(v.Len())

	return v.validators[pick]
}

// CalcProposer calculates the address of the next proposer, from the validator set
func (v *ValidatorSet) CalcProposer(randInt uint64) types.Address {
	pick := randInt % uint64(v.Len())
	return v.validators[pick]
}

// Add adds a new address to the validator set
func (v *ValidatorSet) Add(addr types.Address) {
	v.validators = append(v.validators, addr)
}

// SetValidators
func (v *ValidatorSet) SetValidators(validators []types.Address) {
	v.validators = validators
}

// SetValidators
func (v *ValidatorSet) SetStakeTotal(total *big.Int) {
	v.stakeTotal = total
}

// GetValidators
func (v *ValidatorSet) GetValidators() []types.Address {
	return v.validators
}

// Del removes an address from the validator set
func (v *ValidatorSet) Del(addr types.Address) {
	for indx, i := range v.validators {
		if i == addr {
			v.validators = append(v.validators[:indx], v.validators[indx+1:]...)
		}
	}
}

// Len returns the size of the validator set
func (v *ValidatorSet) Len() int {
	return len(v.validators)
}

// Len returns the size of the validator set
func (v *ValidatorSet) Plen() int {
	return len(v.randPools)
}

// Equal checks if 2 validator sets are equal
func (v *ValidatorSet) Equal(vv []types.Address) bool {
	if len(v.validators) != len(vv) {
		return false
	}

	for indx := range v.validators {
		if v.validators[indx] != vv[indx] {
			return false
		}
	}
	return true
}

// Index returns the index of the passed in address in the validator set.
// Returns -1 if not found
func (v *ValidatorSet) Index(addr types.Address) int {
	for indx, i := range v.validators {
		if i == addr {
			return indx
		}
	}
	return -1
}

// Includes checks if the address is in the validator set
func (v *ValidatorSet) Includes(addr types.Address) bool {
	return v.Index(addr) != -1
}

// MaxFaultyNodes returns the maximum number of allowed faulty nodes (F), based on the current validator set
func (v *ValidatorSet) MaxFaultyNodes() int {
	// N -> number of nodes in IBFT
	// F -> number of faulty nodes
	//
	// N = 3F + 1
	// => F = (N - 1) / 3
	//
	// IBFT tolerates 1 failure with 4 nodes
	// 4 = 3 * 1 + 1
	// To tolerate 2 failures, IBFT requires 7 nodes
	// 7 = 3 * 2 + 1
	// It should always take the floor of the result
	return (len(v.validators) - 1) / 3
}
