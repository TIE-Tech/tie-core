package params

import (
	"encoding/json"
	"github.com/tie-core/common/crypto"
	"github.com/tie-core/consensus/pvbft"
	"math/big"
	"reflect"
	"testing"

	"github.com/tie-core/types"
)

var emptyAddr types.Address

func addr(str string) types.Address {
	return types.StringToAddress(str)
}

func hash(str string) types.Hash {
	return types.StringToHash(str)
}

func TestGenesisAlloc(t *testing.T) {
	cases := []struct {
		input  string
		output map[types.Address]GenesisAccount
	}{
		{
			input: `{
				"0x0000000000000000000000000000000000000000": {
					"balance": "0x11"
				}
			}`,
			output: map[types.Address]GenesisAccount{
				emptyAddr: {
					Balance: big.NewInt(17),
				},
			},
		},
		{
			input: `{
				"0x0000000000000000000000000000000000000000": {
					"balance": "0x11",
					"nonce": "0x100",
					"storage": {
						"` + hash("1").String() + `": "` + hash("3").String() + `",
						"` + hash("2").String() + `": "` + hash("4").String() + `"
					}
				}
			}`,
			output: map[types.Address]GenesisAccount{
				emptyAddr: {
					Balance: big.NewInt(17),
					Nonce:   256,
					Storage: map[types.Hash]types.Hash{
						hash("1"): hash("3"),
						hash("2"): hash("4"),
					},
				},
			},
		},
		{
			input: `{
				"0x0000000000000000000000000000000000000000": {
					"balance": "0x11"
				},
				"0x0000000000000000000000000000000000000001": {
					"balance": "0x12"
				}
			}`,
			output: map[types.Address]GenesisAccount{
				addr("0"): {
					Balance: big.NewInt(17),
				},
				addr("1"): {
					Balance: big.NewInt(18),
				},
			},
		},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			var dec map[types.Address]GenesisAccount
			if err := json.Unmarshal([]byte(c.input), &dec); err != nil {
				if c.output != nil {
					t.Fatal(err)
				}
			} else if !reflect.DeepEqual(dec, c.output) {
				t.Fatal("bad")
			}
		})
	}
}

func TestGenesisX(t *testing.T) {
	cases := []struct {
		input  string
		output *Genesis
	}{
		{
			input: `{
				"difficulty": "0x12",
				"gasLimit": "0x11",
				"alloc": {
					"0x0000000000000000000000000000000000000000": {
						"balance": "0x11"
					},
					"0x0000000000000000000000000000000000000001": {
						"balance": "0x12"
					}
				}
			}`,
			output: &Genesis{
				Difficulty: 18,
				GasLimit:   17,
				Alloc: map[types.Address]*GenesisAccount{
					emptyAddr: {
						Balance: big.NewInt(17),
					},
					addr("1"): {
						Balance: big.NewInt(18),
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			var dec *Genesis
			if err := json.Unmarshal([]byte(c.input), &dec); err != nil {
				if c.output != nil {
					t.Fatal(err)
				}
			} else if !reflect.DeepEqual(dec, c.output) {
				t.Fatal("bad")
			}
		})
	}
}

func TestGenesis_GenesisHeader(t *testing.T) {

	genesis := &Genesis{
		Config:     nil,
		Nonce:      [8]byte{},
		Timestamp:  0,
		ExtraData:  nil,
		GasLimit:   0,
		Difficulty: 0,
		Mixhash:    types.Hash{},
		Coinbase:   types.Address{},
		Alloc:      nil,
		StateRoot:  types.Hash{},
		Number:     0,
		GasUsed:    0,
		ParentHash: types.Hash{},
	}
	header := genesis.GenesisHeader()
	vrfValue, _ := pvbft.CalcVrfSeed(header)
	vrfInfo := &pvbft.SignVRF{
		BlockNumber: 0,
		VrfValue:    vrfValue,
	}
	vrfData, _ := json.Marshal(vrfInfo)
	t.Log("=============>len(vrfData)", len(vrfData))

	hexPrv := "6cac33c3fb1ef818cfd715ddbdb0d5f8f57c056c09d282ab1a211accff2babdb"
	prv, _ := crypto.HexToPrvKey(hexPrv)

	h, err := pvbft.WriteSeal(prv, header, vrfData)
	if err != nil {
		t.Fatal("writeSeal fatal", err)
	}

	extra, _ := pvbft.GetIbftExtra(h)
	t.Log("========>vrfValue", extra.VrfValue)
	t.Log("========>vrfProof", extra.VrfProof)
}