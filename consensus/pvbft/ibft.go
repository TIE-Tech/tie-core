package pvbft

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/tie-core/common/crypto"
	"github.com/tie-core/common/crypto/vrf"
	"github.com/tie-core/core/nodekey"
	"github.com/tie-core/metrics"
	"github.com/tietemp/go-logger"
	"reflect"
	"time"

	"github.com/tie-core/common/hex"
	"github.com/tie-core/common/progress"
	"github.com/tie-core/consensus"
	"github.com/tie-core/consensus/pvbft/proto"
	"github.com/tie-core/p2p"
	"github.com/tie-core/state"
	"github.com/tie-core/syncer"
	"github.com/tie-core/types"
	"google.golang.org/grpc"
	any "google.golang.org/protobuf/types/known/anypb"
)

var (
	ErrInvalidHookParam = errors.New("invalid IBFT hook param passed in")
	ErrMissingHook      = errors.New("missing IBFT hook from mechanism")
)

type blockchainInterface interface {
	Header() *types.Header
	GetHeaderByNumber(i uint64) (*types.Header, bool)
	WriteBlock(block *types.Block) error
	CalculateGasLimit(number uint64) (uint64, error)
}

type txPoolInterface interface {
	Prepare()
	Length() uint64
	Peek() *types.Transaction
	Pop(tx *types.Transaction)
	Drop(tx *types.Transaction)
	Demote(tx *types.Transaction)
	ResetWithHeaders(headers ...*types.Header)
}

type syncerInterface interface {
	Start()
	BestPeer() *syncer.SyncPeer
	BulkSyncWithPeer(p *syncer.SyncPeer, newBlockHandler func(block *types.Block)) error
	WatchSyncWithPeer(p *syncer.SyncPeer, newBlockHandler func(b *types.Block) bool)
	GetSyncProgression() *progress.Progression
	Broadcast(b *types.Block)
}

// Ibft represents the IBFT consensus mechanism object
type Ibft struct {
	sealing bool // Flag indicating if the node is a sealer

	config *consensus.Config // Consensus configuration
	Grpc   *grpc.Server      // gRPC configuration
	state  *currentState     // Reference to the current state

	blockchain blockchainInterface // Interface exposed by the blockchain layer
	executor   *state.Executor     // Reference to the state executor
	closeCh    chan struct{}       // Channel for closing

	validatorKey     *ecdsa.PrivateKey // Private key for the validator
	validatorKeyAddr types.Address

	txpool txPoolInterface // Reference to the transaction pool

	store     *snapshotStore // Snapshot store that keeps track of all snapshots
	epochSize uint64

	msgQueue *msgQueue     // Structure containing different message queues
	updateCh chan struct{} // Update channel

	syncer syncerInterface // Reference to the sync protocol

	network   *p2p.Server // Reference to the networking layer
	transport transport   // Reference to the transport protocol

	operator *operator

	// aux test methods
	forceTimeoutCh bool

	metrics *metrics.CosMetrics

	secretsManager nodekey.SecretsManager

	mechanism ConsensusMechanism // IBFT ConsensusMechanism used (PoA / PoS)

	blockTime time.Duration // Minimum block generation time in seconds

	vrfInfo *VrfInfo

	blockReward *BlockReward
}

// Define the type of the IBFT consensus

type MechanismType string

const (
	// PoS defines the Proof of Stake IBFT type,
	// where the validator set it changed through staking on the Staking SC
	PoS MechanismType = "PoS"
)

// mechanismTypes is the map used for easy string -> mechanism MechanismType lookups
var mechanismTypes = map[string]MechanismType{
	"PoS": PoS,
}

// String is a common method for casting a MechanismType to a string representation
func (t MechanismType) String() string {
	return string(t)
}

// parseType converts a mechanism string representation to a MechanismType
func parseType(mechanism string) (MechanismType, error) {
	// Check if the cast is possible
	castType, ok := mechanismTypes[mechanism]
	if !ok {
		return castType, fmt.Errorf("invalid IBFT mechanism type %s", mechanism)
	}

	return castType, nil
}

// Define constant hook names
const (

	// VerifyHeadersHook defines additional checks that need to happen
	// when verifying the headers
	VerifyHeadersHook = "VerifyHeadersHook"

	// InsertBlockHook defines additional steps that need to happen
	// when inserting a block into the chain
	InsertBlockHook = "InsertBlockHook"

	// SyncStateHook defines the additional snapshot update logic
	// for PoS systems
	SyncStateHook = "SyncStateHook"

	// VerifyBlockHook defines the additional verification steps for the PoS mechanism
	VerifyBlockHook = "VerifyBlockHook"

	// AcceptStateLogHook defines what should be logged out as the status
	// from AcceptState
	AcceptStateLogHook = "AcceptStateLogHook"

	// CalculateProposerHook defines what is the next proposer
	// based on the previous
	CalculateProposerHook = "CalculateProposerHook"
)

type ConsensusMechanism interface {
	// GetType returns the type of IBFT consensus mechanism (PoA / PoS)
	GetType() MechanismType

	// GetHookMap returns the hooks registered with the specific consensus mechanism
	GetHookMap() map[string]func(interface{}) error

	// ShouldWriteTransactions returns whether transactions should be written to a block
	// from the TxPool
	ShouldWriteTransactions(blockNumber uint64) bool

	// initializeHookMap initializes the hook map
	initializeHookMap()
}

// ConsensusMechanismFactory is the factory function to create a consensus mechanism
type ConsensusMechanismFactory func(ibft *Ibft) (ConsensusMechanism, error)

var mechanismBackends = map[MechanismType]ConsensusMechanismFactory{
	PoS: PoSFactory,
}

// runHook runs a specified hook if it is present in the hook map
func (i *Ibft) runHook(hookName string, hookParams interface{}) error {
	// Grab the hook map
	hookMap := i.mechanism.GetHookMap()

	// Grab the actual hook if it's present
	hook, ok := hookMap[hookName]
	if !ok {
		// hook not found, continue
		return ErrMissingHook
	}

	// Run the hook
	return hook(hookParams)
}

// Factory implements the base consensus Factory method
func Factory(
	params *consensus.ConsensusParams,
) (consensus.Consensus, error) {
	var epochSize uint64
	if definedEpochSize, ok := params.Config.Config["epochSize"]; !ok {
		// No epoch size defined, use the default one
		epochSize = types.DefaultEpochSize
	} else {
		// Epoch size is defined, use the passed in one
		readSize, ok := definedEpochSize.(float64)
		if !ok {
			return nil, errors.New("invalid type assertion")
		}

		epochSize = uint64(readSize)
	}

	p := &Ibft{
		config:         params.Config,
		Grpc:           params.Grpc,
		blockchain:     params.Blockchain,
		executor:       params.Executor,
		closeCh:        make(chan struct{}),
		txpool:         params.Txpool,
		state:          newState(),
		network:        params.Network,
		epochSize:      epochSize,
		sealing:        params.Seal,
		metrics:        params.Metrics,
		secretsManager: params.SecretsManager,
		blockTime:      time.Duration(params.BlockTime) * time.Second,
		vrfInfo:        NewVrfInfo(),
		blockReward:    newBlockReward(params.Config.Params.ChainID, params.Config.Path),
	}

	// Initialize the mechanism
	mechanismType, parseErr := parseType(p.config.Config["type"].(string))
	if parseErr != nil {
		return nil, parseErr
	}

	// Grab the mechanism factory and execute it
	mechanismFactory := mechanismBackends[mechanismType]
	mechanism, factoryErr := mechanismFactory(p)
	if factoryErr != nil {
		return nil, factoryErr
	}

	p.mechanism = mechanism

	// Istanbul requires a different header hash function
	types.HeaderHash = istanbulHeaderHash

	p.syncer = syncer.NewSyncer(params.Network, params.Blockchain)
	return p, nil
}

// Start starts the IBFT consensus
func (i *Ibft) Initialize() error {
	// Set up the snapshots
	if err := i.setupSnapshot(); err != nil {
		return err
	}
	return nil
}

// Start starts the IBFT consensus
func (i *Ibft) Start() error {
	// register the grpc operator
	if i.Grpc != nil {
		i.operator = &operator{ibft: i}
		proto.RegisterIbftOperatorServer(i.Grpc, i.operator)
	}

	// Set up the node's validator key
	if err := i.createKey(); err != nil {
		return err
	}

	logger.Info("[BFT] validator key", "addr", i.validatorKeyAddr.String())

	// start the transport protocol
	if err := i.setupTransport(); err != nil {
		return err
	}

	// Start the syncer
	i.syncer.Start()

	// Start the actual IBFT protocol
	go i.start()

	return nil
}

// GetSyncProgression gets the latest sync progression, if any
func (i *Ibft) GetSyncProgression() *progress.Progression {
	return i.syncer.GetSyncProgression()
}

type transport interface {
	Gossip(msg *proto.MessageReq) error
}

// Define the IBFT libp2p protocol
var ibftProto = "/pvbft/0.1"

type gossipTransport struct {
	topic *p2p.Topic
}

// Gossip publishes a new message to the topic
func (g *gossipTransport) Gossip(msg *proto.MessageReq) error {
	return g.topic.Publish(msg)
}

// setupTransport sets up the gossip transport protocol
func (i *Ibft) setupTransport() error {
	// Define a new topic
	topic, err := i.network.NewTopic(ibftProto, &proto.MessageReq{})
	if err != nil {
		return err
	}

	// Subscribe to the newly created topic
	err = topic.Subscribe(func(obj interface{}) {
		msg, ok := obj.(*proto.MessageReq)
		if !ok {
			logger.Error("invalid type assertion for message request")
			return
		}

		if !i.isSealing() {
			// if we are not sealing we do not care about the messages
			// but we need to subscribe to propagate the messages
			return
		}

		// decode sender
		if err = validateMsg(msg); err != nil {
			logger.Error("failed to validate msg", "err", err)
			return
		}

		if msg.From == i.validatorKeyAddr.String() {
			// we are the sender, skip this message since we already
			// relay our own messages internally.
			return
		}
		i.pushMessage(msg)
	})

	if err != nil {
		return err
	}

	i.transport = &gossipTransport{topic: topic}
	return nil
}

// createKey sets the validator's private key from the secrets manager
func (i *Ibft) createKey() error {
	i.msgQueue = newMsgQueue()
	i.closeCh = make(chan struct{})
	i.updateCh = make(chan struct{})

	if i.validatorKey == nil {
		// Check if the validator key is initialized
		var key *ecdsa.PrivateKey

		if i.secretsManager.HasSecret(nodekey.ValidatorKey) {
			// The validator key is present in the secrets manager, load it
			validatorKey, readErr := crypto.ReadConsensusKey(i.secretsManager)
			if readErr != nil {
				return fmt.Errorf("unable to read validator key from Secrets Manager, %w", readErr)
			}
			key = validatorKey
		} else {
			// The validator key is not present in the secrets manager, generate it
			validatorKey, validatorKeyEncoded, genErr := crypto.GenerateAndEncodePrivateKey()
			if genErr != nil {
				return fmt.Errorf("unable to generate validator key for Secrets Manager, %w", genErr)
			}

			// Save the key to the secrets manager
			saveErr := i.secretsManager.SetSecret(nodekey.ValidatorKey, validatorKeyEncoded)
			if saveErr != nil {
				return fmt.Errorf("unable to save validator key to Secrets Manager, %w", saveErr)
			}
			key = validatorKey
		}

		i.validatorKey = key
		i.validatorKeyAddr = crypto.PubKeyToAddress(&key.PublicKey)
	}
	return nil
}

const IbftKeyName = "validator.key"

// start starts the IBFT consensus state machine
func (i *Ibft) start() {
	// consensus always starts in SyncState mode in case it needs
	// to sync with other nodes.
	i.setState(SyncState)

	// Grab the latest header
	header := i.blockchain.Header()
	logger.Debug("[BFT] pvbft.start current sequence", "sequence", header.Number+1)

	for {
		select {
		case <-i.closeCh:
			return
		default: // Default is here because we would block until we receive something in the closeCh
		}

		// Start the state machine loop
		i.runCycle()
	}
}

// runCycle represents the IBFT state machine loop
func (i *Ibft) runCycle() {

	// Log to the console
	if i.state.view != nil {
		logger.Debug("[BFT] runCycle", "state", i.getState(), "sequence", i.state.view.Sequence, "round", i.state.view.Round+1)
	}

	logger.Info("[BFT] runCycle getState start", "block", i.blockchain.Header().Number, "state", i.getState())

	// Based on the current state, execute the corresponding section
	switch i.getState() {
	case AcceptState:
		i.runAcceptState()

	case ValidateState:
		i.runValidateState()

	case RoundChangeState:
		i.runRoundChangeState()

	case SyncState:
		i.runSyncState()
	}
}

// isValidSnapshot checks if the current node is in the validator set for the latest snapshot
func (i *Ibft) isValidSnapshot() bool {
	if !i.isSealing() {
		return false
	}

	// check if we are a validator and enabled
	header := i.blockchain.Header()
	snap, err := i.getSnapshot(header.Number)
	if err != nil {
		return false
	}

	if snap.Includes(i.validatorKeyAddr) {
		i.state.view = &proto.View{
			Sequence: header.Number + 1,
			Round:    0,
		}
		return true
	}
	return false
}

// runSyncState implements the Sync state loop.
//
// It fetches fresh data from the blockchain. Checks if the current node is a validator and resolves any pending blocks
func (i *Ibft) runSyncState() {

	// updateSnapshotCallback keeps the snapshot store in sync with the updated
	// chain data, by calling the SyncStateHook
	updateSnapshotCallback := func(oldLatestNumber uint64) {
		if hookErr := i.runHook(SyncStateHook, oldLatestNumber); hookErr != nil && !errors.Is(hookErr, ErrMissingHook) {
			logger.Error("Unable to run hook", "func", SyncStateHook, "err", hookErr)
		}
	}

	for i.isState(SyncState) {
		oldLatestNumber := i.blockchain.Header().Number
		// try to sync with the best-suited peer
		p := i.syncer.BestPeer()
		if p == nil {
			// if we do not have any peers, and we have been a validator
			// we can start now. In case we start on another fork this will be
			// reverted later
			if i.isValidSnapshot() {
				// initialize the round and sequence
				header := i.blockchain.Header()
				i.state.view = &proto.View{
					Round:    0,
					Sequence: header.Number + 1,
				}
				//Set the round metric
				i.metrics.Rounds.Set(float64(i.state.view.Round))

				i.setState(AcceptState)
				logger.Info("[BFT] runSyncState.setState", "state", AcceptState, "condition", "p=nil, i.isValidSnapshot")
			} else {
				time.Sleep(1 * time.Second)
			}
			continue
		}

		if err := i.syncer.BulkSyncWithPeer(p, func(newBlock *types.Block) {
			// Sync the snapshot state after bulk syncing
			updateSnapshotCallback(oldLatestNumber)
			oldLatestNumber = i.blockchain.Header().Number

			i.txpool.ResetWithHeaders(newBlock.Header)
		}); err != nil {
			logger.Error("failed to bulk sync", "err", err)
			continue
		}

		// if we are a validator we do not even want to wait here
		// we can just move ahead
		if i.isValidSnapshot() {
			i.setState(AcceptState)
			logger.Info("[BFT] runSyncState.isValidSnapshot.setState", "state", AcceptState, "condition", "i.isValidSnapshot")
			continue
		}

		// start watch mode
		var isValidator bool
		i.syncer.WatchSyncWithPeer(p, func(b *types.Block) bool {
			// After each written block, update the snapshot store for PoS.
			// The snapshot store is currently updated for PoA inside the ProcessHeadersHook
			updateSnapshotCallback(oldLatestNumber)
			oldLatestNumber = i.blockchain.Header().Number

			i.syncer.Broadcast(b)
			i.txpool.ResetWithHeaders(b.Header)
			isValidator = i.isValidSnapshot()
			return isValidator
		})

		if isValidator {
			// at this point, we are in sync with the latest chain we know of
			// and we are a validator of that chain so we need to change to AcceptState
			// so that we can start to do some stuff there
			i.setState(AcceptState)
			logger.Info("[BFT] runSyncState.isValidator.setState", "state", AcceptState, "condition", "isValidator")
		}
	}
}

// buildBlock builds the block, based on the passed in snapshot and parent header
func (i *Ibft) buildBlock(snap *Snapshot, parent *types.Header) (*types.Block, error) {
	header := &types.Header{
		ParentHash: parent.Hash,
		Number:     parent.Number + 1,
		Miner:      i.validatorKeyAddr,
		Nonce:      types.Nonce{},
		MixHash:    IstanbulDigest,
		// this is required because blockchain needs difficulty to organize blocks and forks
		Difficulty: parent.Number + 1,
		StateRoot:  types.EmptyRootHash, // this avoids needing state for now
		Sha3Uncles: types.EmptyUncleHash,
		GasLimit:   parent.GasLimit, // Inherit from parent for now, will need to adjust dynamically later.
	}

	// calculate gas limit based on parent header
	gasLimit, err := i.blockchain.CalculateGasLimit(header.Number)
	if err != nil {
		return nil, err
	}

	header.GasLimit = gasLimit

	// calculate millisecond values from consensus custom functions in utils.go file
	// to preserve go backward compatibility as time.UnixMili is available as of go 17

	// set the timestamp
	parentTime := time.Unix(int64(parent.Timestamp), 0)
	headerTime := parentTime.Add(i.blockTime)

	if headerTime.Before(time.Now()) {
		headerTime = time.Now()
	}
	header.Timestamp = uint64(headerTime.Unix())

	// we need to include in the extra field the current set of validators
	putIbftExtraValidators(header, snap.Set)

	transition, err := i.executor.BeginTxn(parent.StateRoot, header, i.validatorKeyAddr)
	if err != nil {
		return nil, err
	}

	// If the mechanism is PoS -> build a regular block if it's not an end-of-epoch block
	// If the mechanism is PoA -> always build a regular block, regardless of epoch
	txns := []*types.Transaction{}
	if i.mechanism.ShouldWriteTransactions(header.Number) {
		txns = i.writeTransactions(gasLimit, transition)
	}

	_, root := transition.Commit()
	header.StateRoot = root
	header.GasUsed = transition.TotalGas()

	// build the block
	block := consensus.BuildBlock(consensus.BuildBlockParams{
		Header:   header,
		Txns:     txns,
		Receipts: transition.Receipts(),
	})

	// write the seal of the block after all the fields are completed
	vrfData := i.vrfInfo.GetInfo(header.Number)
	header, err = writeSeal(i.validatorKey, block.Header, vrfData)
	if err != nil {
		return nil, err
	}
	block.Header = header

	// compute the hash, this is only a provisional hash since the final one
	// is sealed after all the committed seals
	block.Header.ComputeHash()

	logger.Info("[BFT] BUILD block success", "block", header.Number, "txns", len(txns))
	return block, nil
}

type transitionInterface interface {
	Write(txn *types.Transaction) error
	WriteFailedReceipt(txn *types.Transaction) error
}

// writeTransactions writes transactions from the txpool to the transition object
// and returns transactions that were included in the transition (new block)
func (i *Ibft) writeTransactions(gasLimit uint64, transition transitionInterface) []*types.Transaction {
	var transactions []*types.Transaction

	successTxCount := 0
	failedTxCount := 0

	i.txpool.Prepare()

	for {
		tx := i.txpool.Peek()
		if tx == nil {
			break
		}

		if tx.ExceedsBlockGasLimit(gasLimit) {
			if err := transition.WriteFailedReceipt(tx); err != nil {
				failedTxCount++

				i.txpool.Drop(tx)

				continue
			}

			failedTxCount++

			transactions = append(transactions, tx)
			i.txpool.Drop(tx)

			continue
		}

		if err := transition.Write(tx); err != nil {
			logger.Error("transition.Write err", "hash", tx.Hash, "err", err)
			if _, ok := err.(*state.GasLimitReachedTransitionApplicationError); ok { // nolint:errorlint
				break
				//} else if appErr, ok := err.(*state.TransitionApplicationError); ok && appErr.IsRecoverable { // nolint:errorlint
				//	i.txpool.Demote(tx)
			} else {
				failedTxCount++
				i.txpool.Drop(tx)
			}
			continue
		}

		// no errors, pop the tx from the pool
		i.txpool.Pop(tx)
		successTxCount++
		transactions = append(transactions, tx)
	}

	// Block reward transaction
	rewardTx, block := i.witeFixedReward(transition)
	if rewardTx != nil {
		transactions = append(transactions, rewardTx)
	}

	logger.Info("[BFT] writeTransactions from txpool", "block", block, "failed", failedTxCount, "success", successTxCount, "txLen", i.txpool.Length())
	return transactions
}

func (i *Ibft) witeFixedReward(txn transitionInterface) (*types.Transaction, uint64) {
	header := i.blockchain.Header()
	block := header.Number + 1
	reward := i.blockReward.GetReward(uint64(i.blockTime), i.GetEpoch(block))
	tx, err := i.executor.BeginTxn(header.StateRoot, header, header.Miner)
	if err != nil {
		logger.Error("writeTransactions BeginTxn err", "block", header.Number, "err", err)
		return nil, block
	}
	rewardPool := types.StringToAddress(types.RewardPool)
	nonce := tx.Txn().GetNonce(i.validatorKeyAddr)
	rewardTx, err := i.blockReward.rewardTx(i.validatorKey, i.validatorKeyAddr, rewardPool, nonce, reward)
	if err != nil {
		logger.Error("blockReward.rewardTx err", "miner", i.validatorKeyAddr, "err", err)
		return nil, block
	}

	rewardTx.ComputeHash()
	if err = txn.Write(rewardTx); err != nil {
		logger.Error("reward tx Write err", "miner", i.validatorKeyAddr, "err", err)
		return nil, block
	}

	state.NewLock().CleanTag(block)
	state.NewLock().SetHash(block, rewardTx.Hash)
	i.blockReward.settlementReward()
	return rewardTx, block
}

// runAcceptState runs the Accept state loop
//
// The Accept state always checks the snapshot, and the validator set. If the current node is not in the validators set,
// it moves back to the Sync state. On the other hand, if the node is a validator, it calculates the proposer.
// If it turns out that the current node is the proposer, it builds a block,
// and sends preprepare and then prepare messages.
func (i *Ibft) runAcceptState() { // start new round

	// set log output
	logger.Info("[BFT] runAcceptState start", "block", i.state.view.Sequence, "round", i.state.view.Round+1)

	// set consensus_rounds metric output
	i.metrics.Rounds.Set(float64(i.state.view.Round + 1))

	// This is the state in which we either propose a block or wait for the pre-prepare message
	parent := i.blockchain.Header()
	number := parent.Number + 1
	if number != i.state.view.Sequence {
		logger.Error("Sequence not correct", "parent", parent.Number, "sequence", i.state.view.Sequence)
		i.setState(SyncState)
		return
	}

	snap, err := i.getSnapshot(parent.Number)
	if err != nil {
		logger.Error("Cannot find snapshot", "num", parent.Number)
		i.setState(SyncState)
		return
	}

	if !snap.Includes(i.validatorKeyAddr) {
		// we are not a validator anymore, move back to sync state
		logger.Info("[BFT] we are not a validator anymore")
		i.setState(SyncState)
		return
	}

	if hookErr := i.runHook(AcceptStateLogHook, snap); hookErr != nil && !errors.Is(hookErr, ErrMissingHook) {
		logger.Error("Unable to run hook", "func", AcceptStateLogHook, "err", hookErr)
	}

	//Update the No.of validator metric
	i.metrics.Validators.Set(float64(len(snap.Set)))

	// reset round messages
	i.state.resetRoundMsgs()

	// select the proposer of the block
	var lastProposer types.Address
	if parent.Number != 0 {
		lastProposerPub, _ := ecrecoverFromHeader(parent)
		lastProposer = crypto.PubKeyToAddress(lastProposerPub)
	}

	if hookErr := i.runHook(CalculateProposerHook, lastProposer); hookErr != nil && !errors.Is(hookErr, ErrMissingHook) {
		logger.Error("Unable to run hook", "func", CalculateProposerHook, "err", hookErr)
	}

	if i.state.proposer == i.validatorKeyAddr {

		if !i.state.locked {
			// since the state is not locked, we need to build a new block
			i.state.block, err = i.buildBlock(snap, parent)
			if err != nil {
				logger.Error("Failed to build block", "err", err)
				i.setState(RoundChangeState)
				return
			}

			// calculate how much time do we have to wait to mine the block
			delay := time.Until(time.Unix(int64(i.state.block.Header.Timestamp), 0))
			logger.Info("[BFT] runAcceptState wait time", "delay", delay)

			select {
			case <-time.After(delay):
			case <-i.closeCh:
				return
			}
		}

		// send the preprepare message as an RLP encoded block
		i.sendPreprepareMsg()

		// send the prepare message since we are ready to move the state
		i.sendPrepareMsg()

		// move to validation state for new prepare messages
		i.setState(ValidateState)
		return
	}

	logger.Info("[BFT] runAcceptState calculated proposer", "block", number, "proposer", i.state.proposer)

	// we are NOT a proposer for the block. Then, we have to wait
	// for a pre-prepare message from the proposer
	timeout := exponentialTimeout(i.state.view.Round)
	for i.getState() == AcceptState {
		msg, ok := i.getNextMessage(timeout)
		if !ok {
			return
		}

		if msg == nil {
			i.setState(RoundChangeState)
			continue
		}

		if msg.From != i.state.proposer.String() {
			logger.Error("Msg received from wrong proposer")
			continue
		}

		// retrieve the block proposal
		block := &types.Block{}
		if err := block.UnmarshalRLP(msg.Proposal.Value); err != nil {
			logger.Error("Failed to unmarshal block", "err", err)
			i.setState(RoundChangeState)
			return
		}

		if i.state.locked {
			// the state is locked, we need to receive the same block
			if block.Hash() == i.state.block.Hash() {
				// fast-track and send a commit message and wait for validations
				i.sendCommitMsg()
				i.setState(ValidateState)
				logger.Info("[BFT] runAcceptState Commit", "state", ValidateState, "condition", "i.state.locked")
			} else {
				i.handleStateErr(errIncorrectBlockLocked)
			}
		} else {

			// since it's a new block, we have to verify it first
			if err := i.verifyHeaderImpl(snap, parent, block.Header); err != nil {
				logger.Error("Block verification failed", "err", err)
				i.handleStateErr(errBlockVerificationFailed)
				continue
			}

			if hookErr := i.runHook(VerifyBlockHook, block); hookErr != nil && !errors.Is(hookErr, ErrMissingHook) {
				if errors.As(hookErr, &errBlockVerificationFailed) {
					logger.Error("Block verification failed, block at the end of epoch has transactions")
					i.handleStateErr(errBlockVerificationFailed)
				} else {
					logger.Error("Unable to run hook", "func", VerifyBlockHook, "err", hookErr)
				}
				continue
			}

			i.state.block = block

			// send prepare message and wait for validations
			i.sendPrepareMsg()

			i.setState(ValidateState)
			logger.Info("[BFT] RunAcceptState sync block", "block", block.Number())
		}
	}
}

// runValidateState implements the Validate state loop.
// The Validate state is rather simple - all nodes do in this state is read messages
// and add them to their local snapshot state
func (i *Ibft) runValidateState() {

	hasCommitted := false
	sendCommit := func() {
		// at this point either we have enough prepare messages
		// or commit messages so we can lock the block
		i.state.lock()

		if !hasCommitted {
			// send the commit message
			i.sendCommitMsg()
			hasCommitted = true
		}
	}

	timeout := exponentialTimeout(i.state.view.Round)
	for i.getState() == ValidateState {
		msg, ok := i.getNextMessage(timeout)
		if !ok {
			// closing
			return
		}

		if msg == nil {
			i.setState(RoundChangeState)
			continue
		}

		switch msg.Type {
		case proto.MessageReq_Prepare:
			i.state.addPrepared(msg)

		case proto.MessageReq_Commit:
			i.state.addCommitted(msg)

		default:
			logger.Painc("runValidateState no state", "type", reflect.TypeOf(msg.Type))
		}

		if i.state.numPrepared() > i.state.NumValid() {
			// we have received enough pre-prepare messages
			sendCommit()
		}

		if i.state.numCommitted() > i.state.NumValid() {
			// we have received enough commit messages
			sendCommit()

			// try to commit the block
			i.setState(CommitState)
		}
	}

	if i.getState() == CommitState {

		// at this point either if it works or not we need to unlock
		block := i.state.block
		i.state.unlock()

		if err := i.insertBlock(block); err != nil {
			// start a new round with the state unlocked since we need to
			// be able to propose/validate a different block
			logger.Error("runValidateState failed to insert block", "err", err)
			i.handleStateErr(errFailedToInsertBlock)
		} else {
			// update metrics
			i.updateMetrics(block)

			// move ahead to the next block
			i.setState(AcceptState)
		}
	}
}

// updateMetrics will update various metrics based on the given block
// currently we capture No.of Txs and block interval metrics using this function
func (i *Ibft) updateMetrics(block *types.Block) {
	// get previous header
	prvHeader, _ := i.blockchain.GetHeaderByNumber(block.Number() - 1)
	parentTime := time.Unix(int64(prvHeader.Timestamp), 0)
	headerTime := time.Unix(int64(block.Header.Timestamp), 0)

	//Update the block interval metric
	if block.Number() > 1 {
		i.metrics.BlockInterval.Set(
			headerTime.Sub(parentTime).Seconds(),
		)
	}

	//Update the Number of transactions in the block metric
	i.metrics.NumTxs.Set(float64(len(block.Body().Transactions)))
}

func (i *Ibft) insertBlock(block *types.Block) error {
	committedSeals := [][]byte{}
	for _, commit := range i.state.committed {
		// no need to check the format of seal here because writeCommittedSeals will check
		committedSeals = append(committedSeals, hex.MustDecodeHex(commit.Seal))
	}

	header, err := writeCommittedSeals(block.Header, committedSeals)
	if err != nil {
		return errors.New("writeCommittedSeals err:" + err.Error())
	}

	// we need to recompute the hash since we have change extra-data
	block.Header = header
	block.Header.ComputeHash()

	if err := i.blockchain.WriteBlock(block); err != nil {
		return errors.New("WriteBlock:" + err.Error())
	}

	// check change epoch
	if hookErr := i.runHook(InsertBlockHook, header.Number); hookErr != nil && !errors.Is(hookErr, ErrMissingHook) {
		logger.Error("InsertBlockHook err", "block", header.Number, "err", hookErr)
		return hookErr
	}

	logger.Info(
		"[BFT] insert block success",
		"block", i.state.view.Sequence,
		"miner", header.Miner,
		"rounds", i.state.view.Round+1,
		"committed", i.state.numCommitted(),
	)

	// increase the sequence number and reset the round if any
	i.state.view = &proto.View{
		Sequence: header.Number + 1,
		Round:    0,
	}

	// broadcast the new block
	i.syncer.Broadcast(block)

	// after the block has been written we reset the txpool so that
	// the old transactions are removed
	i.txpool.ResetWithHeaders(block.Header)

	return nil
}

var (
	errIncorrectBlockLocked    = fmt.Errorf("block locked is incorrect")
	errBlockVerificationFailed = fmt.Errorf("block verification failed")
	errFailedToInsertBlock     = fmt.Errorf("failed to insert block")
)

func (i *Ibft) handleStateErr(err error) {
	i.state.err = err
	i.setState(RoundChangeState)
}

func (i *Ibft) runRoundChangeState() {

	sendRoundChange := func(round uint64) {
		logger.Debug("[BFT] local round change", "round", round+1)
		// set the new round and update the round metric
		i.state.view.Round = round
		i.metrics.Rounds.Set(float64(round))
		// clean the round
		i.state.cleanRound(round)
		// send the round change message
		i.sendRoundChange()
	}
	sendNextRoundChange := func() {
		sendRoundChange(i.state.view.Round + 1)
	}

	checkTimeout := func() {
		// check if there is any peer that is really advanced and we might need to sync with it first
		if i.syncer != nil {
			bestPeer := i.syncer.BestPeer()
			if bestPeer != nil {
				lastProposal := i.blockchain.Header()
				if bestPeer.Number() > lastProposal.Number {
					logger.Debug("[BFT] it has found a better peer to connect", "local", lastProposal.Number, "remote", bestPeer.Number())
					// we need to catch up with the last sequence
					i.setState(SyncState)

					return
				}
			}
		}

		// otherwise, it seems that we are in sync
		// and we should start a new round
		sendNextRoundChange()
	}

	// if the round was triggered due to an error, we send our own
	// next round change
	if err := i.state.getErr(); err != nil {
		logger.Debug("[BFT] round change handle err", "err", err)
		sendNextRoundChange()
	} else {
		// otherwise, it is due to a timeout in any stage
		// First, we try to sync up with any max round already available
		if maxRound, ok := i.state.maxRound(); ok {
			logger.Debug("[BFT] round change set max round", "round", maxRound)
			sendRoundChange(maxRound)
		} else {
			// otherwise, do your best to sync up
			checkTimeout()
		}
	}

	// create a timer for the round change
	timeout := exponentialTimeout(i.state.view.Round)
	for i.getState() == RoundChangeState {
		msg, ok := i.getNextMessage(timeout)
		if !ok {
			// closing
			return
		}

		if msg == nil {
			logger.Debug("[BFT] round change timeout")
			checkTimeout()
			// update the timeout duration
			timeout = exponentialTimeout(i.state.view.Round)

			continue
		}

		// we only expect RoundChange messages right now
		num := i.state.AddRoundMessage(msg)
		if num == i.state.NumValid() {
			// start a new round immediately
			i.state.view.Round = msg.View.Round
			i.setState(AcceptState)
		} else if num == i.state.vset.MaxFaultyNodes()+1 {
			// weak certificate, try to catch up if our round number is smaller
			if i.state.view.Round < msg.View.Round {
				// update timer
				timeout = exponentialTimeout(i.state.view.Round)
				sendRoundChange(msg.View.Round)
			}
		}
	}
}

// --- com wrappers ---

func (i *Ibft) sendRoundChange() {
	i.gossip(proto.MessageReq_RoundChange)
}

func (i *Ibft) sendPreprepareMsg() {
	i.gossip(proto.MessageReq_Preprepare)
}

func (i *Ibft) sendPrepareMsg() {
	i.gossip(proto.MessageReq_Prepare)
}

func (i *Ibft) sendCommitMsg() {
	i.gossip(proto.MessageReq_Commit)
}

func (i *Ibft) gossip(typ proto.MessageReq_Type) {
	msg := &proto.MessageReq{
		Type: typ,
	}

	// add View
	msg.View = i.state.view.Copy()

	// if we are sending a preprepare message we need to include the proposed block
	if msg.Type == proto.MessageReq_Preprepare {
		msg.Proposal = &any.Any{
			Value: i.state.block.MarshalRLP(),
		}
	}

	// if the message is commit, we need to add the committed seal
	if msg.Type == proto.MessageReq_Commit {
		seal, err := writeCommittedSeal(i.validatorKey, i.state.block.Header)
		if err != nil {
			logger.Error("gossip writeCommittedSeal", "err", err)
			return
		}
		msg.Seal = hex.EncodeToHex(seal)
	}

	if msg.Type != proto.MessageReq_Preprepare {
		// send a copy to ourselves so that we can process this message as well
		msg2 := msg.Copy()
		msg2.From = i.validatorKeyAddr.String()
		i.pushMessage(msg2)
	}

	if err := signMsg(i.validatorKey, msg); err != nil {
		logger.Error("gossip signMsg", "err", err)
		return
	}

	if err := i.transport.Gossip(msg); err != nil {
		logger.Error("failed to gossip", "err", err)
	}
}

// getState returns the current IBFT state
func (i *Ibft) getState() IbftState {
	return i.state.getState()
}

// isState checks if the node is in the passed in state
func (i *Ibft) isState(s IbftState) bool {
	return i.state.getState() == s
}

// setState sets the IBFT state
func (i *Ibft) setState(s IbftState) {
	i.state.setState(s)
}

// forceTimeout sets the forceTimeoutCh flag to true
func (i *Ibft) forceTimeout() {
	i.forceTimeoutCh = true
}

// isSealing checks if the current node is sealing blocks
func (i *Ibft) isSealing() bool {
	return i.sealing
}

// verifyHeaderImpl implements the actual header verification logic
func (i *Ibft) verifyHeaderImpl(snap *Snapshot, parent, header *types.Header) error {
	// ensure the extra data is correctly formatted
	if _, err := getIbftExtra(header); err != nil {
		return err
	}

	if hookErr := i.runHook(VerifyHeadersHook, header.Nonce); hookErr != nil && !errors.Is(hookErr, ErrMissingHook) {
		return hookErr
	}

	if header.MixHash != IstanbulDigest {
		return fmt.Errorf("invalid mixhash")
	}

	if header.Sha3Uncles != types.EmptyUncleHash {
		return fmt.Errorf("invalid sha3 uncles")
	}

	// difficulty has to match number
	if header.Difficulty != header.Number {
		return fmt.Errorf("wrong difficulty")
	}

	// verify the sealer
	pub, err := verifySigner(snap, header)
	if err != nil {
		return err
	}

	// TODO 3 verify
	extra, err := getIbftExtra(header)
	if err != nil {
		return err
	}

	vrfData := make([]byte, 0)
	prvHeader, ok := i.blockchain.GetHeaderByNumber(header.Number - 1)
	if ok {
		seed, err := CalcVrfSeed(prvHeader)
		if err != nil {
			return err
		}
		signVrf := &SignVRF{
			BlockNumber: header.Number,
			VrfValue:    seed,
		}
		vrfData, _ = json.Marshal(signVrf)
	}

	ok, err = vrf.Verify(pub, vrfData, extra.VrfValue, extra.VrfProof)
	if ok == false {
		return errors.New(fmt.Sprintf("VRF verify failed, block: %d", header.Number))
	}

	logger.Info("[BFT] verifyHeaderImpl success", "block", header.Number)
	return err
}

// VerifyHeader wrapper for verifying headers
func (i *Ibft) VerifyHeader(parent, header *types.Header) error {
	snap, err := i.getSnapshot(parent.Number)
	if err != nil {
		return err
	}

	if err := i.verifyHeaderImpl(snap, parent, header); err != nil {
		return err
	}

	// verify the committed seals
	if err := verifyCommitedFields(snap, header); err != nil {
		return err
	}

	// process the new block in order to update the snapshot
	if err := i.processHeaders([]*types.Header{header}); err != nil {
		return err
	}
	return nil
}

// GetBlockCreator retrieves the block signer from the extra data field
func (i *Ibft) GetBlockCreator(header *types.Header) (types.Address, error) {
	signerPub, err := ecrecoverFromHeader(header)
	if err != nil {
		return types.Address{}, err
	}
	return crypto.PubKeyToAddress(signerPub), nil
}

// Close closes the IBFT consensus mechanism, and does write back to disk
func (i *Ibft) Close() error {
	close(i.closeCh)

	if i.config.Path != "" {
		err := i.store.saveToPath(i.config.Path)
		if err != nil {
			return err
		}
	}
	return nil
}

// getNextMessage reads a new message from the message queue
func (i *Ibft) getNextMessage(timeout time.Duration) (*proto.MessageReq, bool) {
	timeoutCh := time.After(timeout)

	for {
		msg := i.msgQueue.readMessage(i.getState(), i.state.view)
		if msg != nil {
			return msg.obj, true
		}

		if i.forceTimeoutCh {
			i.forceTimeoutCh = false
			return nil, true
		}

		// wait until there is a new message or
		// someone closes the stopCh (i.e. timeout for round change)
		select {
		case <-timeoutCh:
			logger.Info("[BFT] unable to read new message from the message queue", "timeout expired", timeout)
			return nil, true
		case <-i.closeCh:
			return nil, false
		case <-i.updateCh:
		}
	}
}

// pushMessage pushes a new message to the message queue
func (i *Ibft) pushMessage(msg *proto.MessageReq) {
	task := &msgTask{
		view: msg.View,
		msg:  protoTypeToMsg(msg.Type),
		obj:  msg,
	}
	i.msgQueue.pushMessage(task)

	select {
	case i.updateCh <- struct{}{}:
	default:
	}
}