package pvbft

import (
	"crypto/ecdsa"
	"encoding/json"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/tie-core/common/hex"
	"github.com/tie-core/state"
	"github.com/tie-core/types"
	"github.com/tietemp/go-logger"
	"math/big"
)

const RewardTotal = 660000000

var (
	rewardKey     = []byte("RewardKey")
	remainingKey  = []byte("RemainingKey")
	rewardListKey = []byte("RewardListKey")
)

type BlockReward struct {
	db         *leveldb.DB
	reward     *big.Int // 每块奖励额
	epochTotal *big.Int // 当前代奖励总额
	remaining  *big.Int // 当前代奖励剩余
	rewardList []*big.Int
	signer     *state.EIP155Signer
}

func newBlockReward(chainid int, path string) *BlockReward {
	ldb, _ := leveldb.OpenFile(path+"/reward", nil)
	blockReward := &BlockReward{
		db:         ldb,
		reward:     big.NewInt(0),
		epochTotal: big.NewInt(0),
		remaining:  big.NewInt(0),
		rewardList: make([]*big.Int, 0),
		signer:     state.NewEIP155Signer(uint64(chainid)),
	}
	blockReward.initReward()
	return blockReward
}

// initReward
// 1 billion in 20 years, halved every two years
func (br *BlockReward) initReward() {
	total := RewardTotal
	rewardList := make([]int, 0)
	for n := uint64(1); n < 10; n++ {
		total -= total / 2
		rewardList = append(rewardList, total)
	}
	rewardList = append(rewardList, total)

	for _, v := range rewardList {
		bigV := new(big.Int).Mul(big.NewInt(int64(v)), big.NewInt(types.WEI))
		br.rewardList = append(br.rewardList, bigV)
	}
	value, _ := json.Marshal(br.rewardList)
	br.db.Put(rewardListKey, value, nil)
}

// calcEpochTotal
func (br *BlockReward) calcEpochTotal(blockTime, currentEpoch uint64) {
	if br.rewardList == nil {
		br.rewardList = br.getRewardList()
	}

	// Calculate the distribution amount every two years
	// The quota is halved every two years
	for k, _ := range br.rewardList {

		// uint64((k+1) * 2 * types.OneYearEpoch)
		// Number of cycles per two years
		if currentEpoch <= uint64((k+1)*2*types.OneYearEpoch) {
			br.epochTotal = br.rewardList[k]
			break
		}
	}

	epochTotal := br.epochTotal
	br.remaining = epochTotal
	outBlock := 86400 * 365 * 2 / (blockTime / 1e9)
	noWeiTotal := new(big.Int).Div(epochTotal, big.NewInt(types.WEI))
	br.reward = new(big.Int).Div(noWeiTotal, new(big.Int).SetUint64(outBlock))
	br.reward.Mul(br.reward, big.NewInt(types.WEI))
	br.db.Put(rewardKey, br.reward.Bytes(), nil)
	br.db.Put(remainingKey, br.remaining.Bytes(), nil)
	logger.Debug("[BFT] Calculate block rewards", "epochTotal", br.epochTotal, "reward", br.reward)
}

func (br *BlockReward) GetReward(blockTime, currentEpoch uint64) *big.Int {
	if br.getRemaining().Cmp(br.reward) < 0 {
		br.calcEpochTotal(blockTime, currentEpoch)
	}
	if br.reward.Cmp(big.NewInt(0)) == 0 {
		reward := br.getReward()
		if reward.Cmp(big.NewInt(0)) > 0 {
			br.reward = reward
		} else {
			br.calcEpochTotal(blockTime, currentEpoch)
		}
	}
	logger.Debug("[BFT] GetReward", "reward", br.reward, "remaining", br.remaining)
	return br.reward
}

func (br *BlockReward) settlementReward() {
	if br.remaining.Cmp(big.NewInt(0)) == 0 {
		br.remaining = br.getRemaining()
	}
	br.remaining = br.remaining.Sub(br.remaining, br.reward)
	br.db.Put(remainingKey, br.remaining.Bytes(), nil)
}

func (br *BlockReward) getRewardList() []*big.Int {
	value, _ := br.db.Get(rewardListKey, nil)
	rewardList := make([]*big.Int, 0)
	json.Unmarshal(value, rewardList)
	return rewardList
}

func (br *BlockReward) getReward() *big.Int {
	value, _ := br.db.Get(rewardKey, nil)
	return new(big.Int).SetBytes(value)
}

func (br *BlockReward) getRemaining() *big.Int {
	value, _ := br.db.Get(remainingKey, nil)
	return new(big.Int).SetBytes(value)
}

func (br *BlockReward) rewardTx(prv *ecdsa.PrivateKey, miner, rewardPool types.Address, nonce uint64, amount *big.Int) (*types.Transaction, error) {
	data, _ := hex.DecodeHex(types.FixedRewardMethod)
	tx := &types.Transaction{
		Nonce:    nonce,
		From:     miner,
		To:       &rewardPool,
		Value:    amount,
		Gas:      21000,
		GasPrice: big.NewInt(0),
		Input:    data,
	}
	return br.signer.SignTx(tx, prv)
}