package pvbft

import (
	"encoding/json"
	"fmt"
	"github.com/tie-core/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tie-core/consensus/pvbft/proto"
)

func TestState_FaultyNodes(t *testing.T) {
	cases := []struct {
		Network, Faulty uint64
	}{
		{1, 0},
		{2, 0},
		{3, 0},
		{4, 1},
		{5, 1},
		{6, 1},
		{7, 2},
		{8, 2},
		{9, 2},
	}
	for _, c := range cases {
		pool := newTesterAccountPool(int(c.Network))
		vals := pool.ValidatorSet()
		assert.Equal(t, (len(vals)-1)/3, int(c.Faulty))
	}
}

func TestState_AddMessages(t *testing.T) {
	pool := newTesterAccountPool()
	pool.add("A", "B", "C", "D")

	c := newState()
	c.vset = NewValidatorSet()

	msg := func(acct string, typ proto.MessageReq_Type, round ...uint64) *proto.MessageReq {
		msg := &proto.MessageReq{
			From: pool.get(acct).Address().String(),
			Type: typ,
			View: &proto.View{Round: 0},
		}
		r := uint64(0)

		if len(round) > 0 {
			r = round[0]
		}

		msg.View.Round = r

		return msg
	}

	// -- test committed messages --
	c.addMessage(msg("A", proto.MessageReq_Commit))
	c.addMessage(msg("B", proto.MessageReq_Commit))
	c.addMessage(msg("B", proto.MessageReq_Commit))

	assert.Equal(t, c.numCommitted(), 2)

	// -- test prepare messages --
	c.addMessage(msg("C", proto.MessageReq_Prepare))
	c.addMessage(msg("C", proto.MessageReq_Prepare))
	c.addMessage(msg("D", proto.MessageReq_Prepare))

	assert.Equal(t, c.numPrepared(), 2)
}

func TestNewValidatorSet(t *testing.T) {
	address := types.StringToAddress("0x69e134f261E27FECea7CBdD3157E59913ae4e468")
	address2 := types.StringToAddress("0xD7A35Ad8f6995a57671247104675A972d1Cc256b")
	validators := []types.Address{address, address2}
	vset := NewValidatorSet()
	vset.SetValidators(validators)

	fmt.Println(vset.GetValidators())

	b, _ := json.Marshal(vset)
	fmt.Println(string(b))
}