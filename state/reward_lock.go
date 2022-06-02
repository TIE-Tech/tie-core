package state

import (
	"github.com/TIE-Tech/tie-core/types"
	"sync"
)

type RewardLock struct {
	mu         sync.Mutex
	fixedLock  map[uint64]int
	rewardHash map[uint64]types.Hash
}

var (
	lockOnce   sync.Once
	rewardLock *RewardLock
)

func NewLock() *RewardLock {
	lockOnce.Do(func() {
		rewardLock = &RewardLock{
			mu:         sync.Mutex{},
			fixedLock:  make(map[uint64]int),
			rewardHash: make(map[uint64]types.Hash),
		}
	})
	return rewardLock
}

func (r *RewardLock) SetHash(block uint64, hash types.Hash) {
	r.mu.Lock()
	r.rewardHash[block] = hash
	r.mu.Unlock()
}

func (r *RewardLock) GetHash(block uint64) types.Hash {
	return r.rewardHash[block]
}

func (r *RewardLock) DelHash(block uint64) {
	r.mu.Lock()
	delete(r.rewardHash, block)
	r.mu.Unlock()
}

func (r *RewardLock) TagCount(block uint64) {
	r.mu.Lock()
	r.fixedLock[block]++
	r.mu.Unlock()
}

func (r *RewardLock) CleanTag(block uint64) {
	delete(r.fixedLock, block)
}

func (r *RewardLock) Tag(block uint64) int {
	return r.fixedLock[block]
}
