package types

import (
	"encoding/hex"
	"github.com/TIE-Tech/tie-core/common/crypto/keccak"
	"math/big"
	"strings"
	"sync/atomic"
)

type Transaction struct {
	Nonce    uint64
	GasPrice *big.Int
	Gas      uint64
	To       *Address
	Value    *big.Int
	Input    []byte
	V        *big.Int
	R        *big.Int
	S        *big.Int
	Hash     Hash
	From     Address

	// Cache
	size atomic.Value
}

const (
	WithdrawFeeMethod = "070f468d" // withdrawTxFee
	FixedRewardMethod = "57305920" // fixedReward
)

func (t *Transaction) IsContractCreation() bool {
	return t.To == nil
}

func (t *Transaction) IsWithdrawFee() bool {
	return strings.Count(hex.EncodeToString(t.Input), WithdrawFeeMethod) > 0
}

func (t *Transaction) IsFixedRewardTx() bool {
	return strings.Count(hex.EncodeToString(t.Input), FixedRewardMethod) > 0
}

// ComputeHash computes the hash of the transaction
func (t *Transaction) ComputeHash() *Transaction {
	ar := marshalArenaPool.Get()
	hash := keccak.DefaultKeccakPool.Get()

	v := t.MarshalRLPWith(ar)
	hash.WriteRlp(t.Hash[:0], v)

	marshalArenaPool.Put(ar)
	keccak.DefaultKeccakPool.Put(hash)

	return t
}

func (t *Transaction) Copy() *Transaction {
	tt := new(Transaction)
	*tt = *t

	tt.GasPrice = new(big.Int)
	tt.GasPrice.Set(t.GasPrice)

	tt.Value = new(big.Int)
	tt.Value.Set(t.Value)

	if t.R != nil {
		tt.R = new(big.Int)
		tt.R = big.NewInt(0).SetBits(t.R.Bits())
	}

	if t.S != nil {
		tt.S = new(big.Int)
		tt.S = big.NewInt(0).SetBits(t.S.Bits())
	}

	tt.Input = make([]byte, len(t.Input))
	copy(tt.Input[:], t.Input[:])

	return tt
}

// Cost returns gas * gasPrice + value
func (t *Transaction) Cost() *big.Int {
	total := new(big.Int).Mul(t.GasPrice, new(big.Int).SetUint64(t.Gas))
	total.Add(total, t.Value)

	return total
}

func (t *Transaction) Size() uint64 {
	if size := t.size.Load(); size != nil {
		sizeVal, ok := size.(uint64)
		if !ok {
			return 0
		}

		return sizeVal
	}

	size := uint64(len(t.MarshalRLP()))
	t.size.Store(size)

	return size
}

func (t *Transaction) ExceedsBlockGasLimit(blockGasLimit uint64) bool {
	return t.Gas > blockGasLimit
}

func (t *Transaction) IsUnderpriced(priceLimit uint64) bool {
	return t.GasPrice.Cmp(big.NewInt(0).SetUint64(priceLimit)) < 0
}
