package pvbft

import (
	"encoding/json"
	"github.com/tie-core/common/crypto"
	"github.com/tie-core/types"
)

// SignVRF
type SignVRF struct {
	BlockNumber uint64
	VrfValue    []byte
}

// CalcVRFData
type CalcVRFData struct {
	BlockNumber uint64
	StateRoot   types.Hash
	ParentVRF   []byte
	ParentHash  types.Hash
}

// getVrfValue
func getVrfValue(h *types.Header) ([]byte, []byte, error) {
	extra, err := getIbftExtra(h)
	if err != nil {
		return nil, nil, err
	}
	return extra.VrfValue, extra.VrfProof, nil
}

// CalcVrfSeed Calculate vrf seed
func CalcVrfSeed(h *types.Header) ([]byte, error) {
	prvVrfValue, _, _ := getVrfValue(h)
	if prvVrfValue == nil {
		prvVrfValue = make([]byte, 64)
	}
	calcVrfData := &CalcVRFData{
		BlockNumber: h.Number + 1,
		StateRoot:   h.StateRoot,
		ParentVRF:   prvVrfValue,
		ParentHash:  h.Hash,
	}
	data, err := json.Marshal(calcVrfData)
	if err != nil {
		return nil, err
	}

	t := crypto.Keccak256(data)
	f := crypto.Keccak256(t[:])
	return f, nil
}