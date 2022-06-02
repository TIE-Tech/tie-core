package consensus

import (
	"context"
	"github.com/tie-core/core/nodekey"
	"github.com/tie-core/metrics"
	"github.com/tie-core/params"
	"log"

	"github.com/tie-core/common/progress"
	"github.com/tie-core/core"
	"github.com/tie-core/p2p"
	"github.com/tie-core/state"
	"github.com/tie-core/txpool"
	"github.com/tie-core/types"
	"google.golang.org/grpc"
)

// Consensus is the public interface for consensus mechanism
// Each consensus mechanism must implement this interface in order to be valid
type Consensus interface {
	// VerifyHeader verifies the header is correct
	VerifyHeader(parent, header *types.Header) error

	// GetBlockCreator retrieves the block creator (or signer) given the block header
	GetBlockCreator(header *types.Header) (types.Address, error)

	// GetSyncProgression retrieves the current sync progression, if any
	GetSyncProgression() *progress.Progression

	// Initialize initializes the consensus (e.g. setup data)
	Initialize() error

	// Start starts the consensus and servers
	Start() error

	// Close closes the connection
	Close() error
}

// Config is the configuration for the consensus
type Config struct {
	// Logger to be used by the backend
	Logger *log.Logger

	// Params are the params of the chain and the consensus
	Params *params.Params

	// Config defines specific configuration parameters for the backend
	Config map[string]interface{}

	// Path is the directory path for the consensus protocol tos tore information
	Path string
}

type ConsensusParams struct {
	Context        context.Context
	Seal           bool
	Config         *Config
	Txpool         *txpool.TxPool
	Network        *p2p.Server
	Blockchain     *blockchain.Blockchain
	Executor       *state.Executor
	Grpc           *grpc.Server
	Metrics        *metrics.CosMetrics
	SecretsManager nodekey.SecretsManager
	BlockTime      uint64
}

// Factory is the factory function to create a discovery backend
type Factory func(*ConsensusParams) (Consensus, error)