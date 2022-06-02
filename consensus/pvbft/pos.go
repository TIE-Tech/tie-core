package pvbft

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/TIE-Tech/go-logger"
	"github.com/TIE-Tech/tie-core/common/crypto/vrf"
	"github.com/TIE-Tech/tie-core/contracts/staking"
	"github.com/TIE-Tech/tie-core/state"
	"github.com/TIE-Tech/tie-core/types"
	"math/big"
)

// PoSMechanism defines specific hooks for the Proof of Stake IBFT mechanism
type PoSMechanism struct {
	// Reference to the main IBFT implementation
	ibft *Ibft

	// hookMap is the collection of registered hooks
	hookMap map[string]func(interface{}) error

	// Used for easy lookups
	mechanismType MechanismType
}

// PoSFactory initializes the required data
// for the Proof of Stake mechanism
func PoSFactory(ibft *Ibft) (ConsensusMechanism, error) {
	pos := &PoSMechanism{
		mechanismType: PoS,
		ibft:          ibft,
	}

	pos.initializeHookMap()
	return pos, nil
}

// GetType implements the ConsensusMechanism interface method
func (pos *PoSMechanism) GetType() MechanismType {
	return pos.mechanismType
}

// GetHookMap implements the ConsensusMechanism interface method
func (pos *PoSMechanism) GetHookMap() map[string]func(interface{}) error {
	return pos.hookMap
}

// calculateProposerHook calculates the next proposer based on the last
func (pos *PoSMechanism) calculateProposerHook(param interface{}) error {

	// TODO 1 generator
	header := pos.ibft.blockchain.Header()
	seed, err := CalcVrfSeed(header)
	if err != nil {
		return err
	}
	seedRandInt := vrf.HashToBigInt(seed)
	pos.ibft.state.CalcProposer(seedRandInt)

	signVrf := &SignVRF{
		BlockNumber: header.Number + 1,
		VrfValue:    seed,
	}
	vrfData, _ := json.Marshal(signVrf)
	pos.ibft.vrfInfo.SetInfo(signVrf.BlockNumber, vrfData)
	return nil
}

// acceptStateLogHook logs the current snapshot
func (pos *PoSMechanism) acceptStateLogHook(snapParam interface{}) error {
	// Cast the param to a *Snapshot
	snap, ok := snapParam.(*Snapshot)
	if !ok {
		return ErrInvalidHookParam
	}

	// Log the info message
	logger.Info("[POS] current snapshot", "validators", len(snap.Set))
	return nil
}

// insertBlockHook checks if the block is the last block of the epoch,
// in order to update the validator set
func (pos *PoSMechanism) insertBlockHook(numberParam interface{}) error {
	headerNumber, ok := numberParam.(uint64)
	if !ok {
		return ErrInvalidHookParam
	}

	if pos.ibft.IsLastOfEpoch(headerNumber) {
		if err := pos.ibft.updateValidators(headerNumber); err != nil {
			return errors.New("updateValidators err," + err.Error())
		}
	}
	return nil
}

// syncStateHook keeps the snapshot store up to date for a range of synced blocks
func (pos *PoSMechanism) syncStateHook(referenceNumber interface{}) error {
	oldLatestNumber, ok := referenceNumber.(uint64)
	if !ok {
		return ErrInvalidHookParam
	}

	// For the block range, update the snapshot store accordingly if an epoch occurred in the range
	if err := pos.ibft.batchUpdateValidators(
		oldLatestNumber+1,
		pos.ibft.blockchain.Header().Number,
	); err != nil {
		logger.Error("SyncStateHook failed to bulk update validators", "err", err)
	}
	return nil
}

// verifyBlockHook checks if the block is an epoch block and if it has any transactions
func (pos *PoSMechanism) verifyBlockHook(blockParam interface{}) error {
	block, ok := blockParam.(*types.Block)
	if !ok {
		return ErrInvalidHookParam
	}

	if pos.ibft.IsLastOfEpoch(block.Number()) && len(block.Transactions) > 0 {
		return errBlockVerificationFailed
	}
	return nil
}

// initializeHookMap registers the hooks that the PoS mechanism
// should have
func (pos *PoSMechanism) initializeHookMap() {
	// Create the hook map
	pos.hookMap = make(map[string]func(interface{}) error)

	// Register the AcceptStateLogHook
	pos.hookMap[AcceptStateLogHook] = pos.acceptStateLogHook

	// Register the InsertBlockHook
	pos.hookMap[InsertBlockHook] = pos.insertBlockHook

	// Register the SyncStateHook
	pos.hookMap[SyncStateHook] = pos.syncStateHook

	// Register the VerifyBlockHook
	pos.hookMap[VerifyBlockHook] = pos.verifyBlockHook

	// Register the CalculateProposerHook
	pos.hookMap[CalculateProposerHook] = pos.calculateProposerHook
}

// ShouldWriteTransactions indicates if transactions should be written to a block
func (pos *PoSMechanism) ShouldWriteTransactions(blockNumber uint64) bool {
	// Epoch blocks should be empty
	return !pos.ibft.IsLastOfEpoch(blockNumber)
}

// getNextValidators is a common function for fetching the validator set
// from the Staking SC
func (i *Ibft) getNextValidators(header *types.Header) ([]types.Address, error) {
	transition, err := i.executor.BeginTxn(header.StateRoot, header, types.ZeroAddress)
	if err != nil {
		return nil, err
	}
	return staking.QueryValidators(transition, i.validatorKeyAddr)
}

// updateSnapshotValidators updates validators in snapshot at given height
func (i *Ibft) updateValidators(block uint64) error {
	header, ok := i.blockchain.GetHeaderByNumber(block)
	if !ok {
		return errors.New(fmt.Sprintf("number %d header not found", block))
	}

	validators, err := i.getNextValidators(header)
	if err != nil {
		return err
	}

	logger.Info("[BFT] updateValidators", "vlen", len(validators))

	snap, err := i.getSnapshot(header.Number)
	if err != nil {
		return err
	}

	if snap == nil {
		return fmt.Errorf("cannot find snapshot at %d", header.Number)
	}

	if !snap.SetEqual(validators) {
		newSnap := snap.Copy()
		newSnap.Set = validators
		newSnap.Number = header.Number
		newSnap.Hash = header.Hash.String()

		if snap.Number != header.Number {
			i.store.add(newSnap)
		} else {
			i.store.replace(newSnap)
		}
	}
	i.distributeFeeRewards(header, validators)
	return nil
}

// batchUpdateValidators updates the validator set based on the passed in block range
func (i *Ibft) batchUpdateValidators(from, to uint64) error {
	for n := from; n <= to; n++ {
		if i.IsLastOfEpoch(n) {
			if err := i.updateValidators(n); err != nil {
				return err
			}
		}
	}
	return nil
}

// GetEpoch returns the current epoch
func (i *Ibft) GetEpoch(number uint64) uint64 {
	if number%i.epochSize == 0 {
		return number / i.epochSize
	}
	return number/i.epochSize + 1
}

// IsLastOfEpoch checks if the block number is the last of the epoch
func (i *Ibft) IsLastOfEpoch(number uint64) bool {
	return number > 0 && number%i.epochSize == 0
}

// distributeFeeRewards distribute fee rewards
func (i *Ibft) distributeFeeRewards(header *types.Header, validators []types.Address) error {
	transition, err := i.executor.BeginTxn(header.StateRoot, header, types.ZeroAddress)
	if err != nil {
		return err
	}

	stakeTotal := big.NewInt(0)
	for _, validator := range validators {
		bigAmount, err := staking.QueryAccountStake(transition, validator)
		if err != nil {
			return err
		}
		stakeTotal = stakeTotal.Add(stakeTotal, bigAmount)
		i.state.vset.AppendStakeList(validator, bigAmount)
	}
	i.state.vset.SetStakeTotal(stakeTotal)

	feeTotal := transition.GetBalance(state.FeePool)
	taximeter := state.NewValidatorFee().GetTaximeter()
	actualAmount := feeTotal.Sub(feeTotal, taximeter)
	if actualAmount.Cmp(big.NewInt(0)) > 0 {
		i.state.vset.DistributeRewardsByRate(actualAmount)
	}
	logger.Info("[BFT] distributeRewards success", "stakeTotal", stakeTotal, "actual", actualAmount)
	return nil
}
