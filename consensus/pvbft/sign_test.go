package pvbft

import (
	"testing"

	"github.com/TIE-Tech/tie-core/consensus/pvbft/proto"
	"github.com/TIE-Tech/tie-core/types"
	"github.com/stretchr/testify/assert"
)

func TestSign_Sealer(t *testing.T) {
	pool := newTesterAccountPool()
	pool.add("A")

	snap := &Snapshot{
		Set: pool.ValidatorSet(),
	}

	h := &types.Header{}
	putIbftExtraValidators(h, pool.ValidatorSet())

	// non-validator address
	pool.add("X")

	badSealedBlock, _ := writeSeal(pool.get("X").priv, h, nil)
	_, err := verifySigner(snap, badSealedBlock)
	assert.Error(t, err)

	// seal the block with a validator
	goodSealedBlock, _ := writeSeal(pool.get("A").priv, h, nil)
	_, err = verifySigner(snap, goodSealedBlock)
	assert.NoError(t, err)
}

func TestSign_CommittedSeals(t *testing.T) {
	pool := newTesterAccountPool()
	pool.add("A", "B", "C", "D", "E")

	snap := &Snapshot{
		Set: pool.ValidatorSet(),
	}

	h := &types.Header{}
	putIbftExtraValidators(h, pool.ValidatorSet())

	// non-validator address
	pool.add("X")

	buildCommittedSeal := func(accnt []string) error {
		seals := [][]byte{}

		for _, accnt := range accnt {
			seal, err := writeCommittedSeal(pool.get(accnt).priv, h)

			assert.NoError(t, err)

			seals = append(seals, seal)
		}

		sealed, err := writeCommittedSeals(h, seals)

		assert.NoError(t, err)

		return verifyCommitedFields(snap, sealed)
	}

	// Correct
	assert.NoError(t, buildCommittedSeal([]string{"A", "B", "C"}))

	// Failed - Repeated signature
	assert.Error(t, buildCommittedSeal([]string{"A", "A"}))

	// Failed - Non validator signature
	assert.Error(t, buildCommittedSeal([]string{"A", "X"}))

	// Failed - Not enough signatures
	assert.Error(t, buildCommittedSeal([]string{"A"}))
}

func TestSign_Messages(t *testing.T) {
	pool := newTesterAccountPool()
	pool.add("A")

	msg := &proto.MessageReq{}
	assert.NoError(t, signMsg(pool.get("A").priv, msg))
	assert.NoError(t, validateMsg(msg))

	assert.Equal(t, msg.From, pool.get("A").Address().String())
}
