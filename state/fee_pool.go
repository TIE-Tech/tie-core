package state

import (
	"github.com/tie-core/types"
	"github.com/tietemp/go-logger"
	"math/big"
	"sync"
)

type ValidatorFee struct {
	mu        sync.RWMutex
	taximeter *big.Int
	feePool   map[types.Address]*big.Int
}

var (
	once         sync.Once
	validatorFee *ValidatorFee
	FeePool      = types.StringToAddress(types.TxFeePool)
)

// NewValidatorFee
func NewValidatorFee() *ValidatorFee {
	once.Do(func() {
		validatorFee = &ValidatorFee{
			mu:        sync.RWMutex{},
			taximeter: big.NewInt(0),
			feePool:   make(map[types.Address]*big.Int),
		}
	})
	return validatorFee
}

// SetValidatorFee
func (v *ValidatorFee) SetValidatorFee(validator types.Address, amount *big.Int) {
	v.mu.Lock()
	defer v.mu.Unlock()

	temp := v.feePool[validator]
	if temp == nil {
		temp = big.NewInt(0)
	}
	temp.Add(temp, amount)
	v.feePool[validator] = temp
	v.taximeter.Add(v.taximeter, amount)
	logger.Debug("[BFT] SetValidatorFee", "validator", validator, "fee", amount, "total", temp, "taximeter", v.taximeter)
}

// IsHaveReward
func (v *ValidatorFee) IsHaveReward(validator types.Address) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.feePool[validator] == nil {
		return false
	}
	return v.feePool[validator].Cmp(big.NewInt(0)) > 0
}

func (v *ValidatorFee) SubTaximeter(amount *big.Int) {
	v.mu.Lock()
	v.taximeter.Sub(v.taximeter, amount)
	v.mu.Unlock()
}

func (v *ValidatorFee) GetTaximeter() *big.Int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.taximeter
}

// GetFeeReward
func (v *ValidatorFee) GetFeeReward(validator types.Address) *big.Int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.feePool[validator]
}