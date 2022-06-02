package vrf

import (
	"github.com/tie-core/common/crypto"
	"github.com/tie-core/types"
	"strconv"
	"testing"
)

func TestVrf(t *testing.T) {
	prv, _ := crypto.GenerateKey()
	vrf, proof, err := Vrf(prv, []byte("test"))
	if err != nil {
		t.Fatal("VRF error, ", err)
	}
	vrfHash := types.BytesToHash(vrf)
	t.Log("vrf hash: ", vrfHash.String())

	ok, err := Verify(&prv.PublicKey, []byte("test"), vrf, proof)
	if err != nil {
		t.Fatal("verify error: ", err)
	}
	t.Log("verify success: ", ok)
}

func TestRandNodeId(t *testing.T) {
	prv, _ := crypto.GenerateKey()

	for i := 1; i <= 100; i++ {
		s := strconv.Itoa(i)
		vrf, _, err := Vrf(prv, []byte(s))
		if err != nil {
			t.Fatal("VRF error, ", err)
		}
		vrfHash := types.BytesToHash(vrf)
		b := HashToBigInt(vrfHash.Bytes())
		t.Log("=======>", b.Uint64()%4)
	}
}

func BenchmarkVrf(b *testing.B) {

	prv, _ := crypto.GenerateKey()

	for i := 0; i < b.N; i++ {
		Vrf(prv, []byte("test"))
	}
}