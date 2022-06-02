package pvbft

import (
	"sync"
)

type VrfInfo struct {
	block   uint64
	vrfData []byte
	mu      sync.Mutex
}

var (
	vrfInfo *VrfInfo
	onceInc sync.Once
)

func NewVrfInfo() *VrfInfo {
	onceInc.Do(func() {
		vrfInfo = &VrfInfo{
			block:   0,
			vrfData: make([]byte, 0),
			mu:      sync.Mutex{},
		}
	})
	return vrfInfo
}

func (v *VrfInfo) SetInfo(block uint64, data []byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.block = block
	v.vrfData = data
}

func (v *VrfInfo) GetInfo(block uint64) []byte {
	if v.block == block {
		return v.vrfData
	}
	return make([]byte, 0)
}
