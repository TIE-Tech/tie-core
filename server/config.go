package server

import (
	"github.com/tie-core/core/nodekey"
	"github.com/tie-core/params"
	"github.com/tie-core/types"
	"net"

	"github.com/tie-core/p2p"
)

// Config is used to parametrize the minimal client
type Config struct {
	Chain *params.Chain

	JSONRPCAddr    *net.TCPAddr
	GRPCAddr       *net.TCPAddr
	LibP2PAddr     *net.TCPAddr
	Telemetry      *Telemetry
	Network        *p2p.Config
	DataDir        string
	Seal           bool
	PriceLimit     uint64
	MaxSlots       uint64
	SecretsManager *nodekey.SecretsManagerConfig
	RestoreFile    *string
	BlockTime      uint64
}

// DefaultConfig returns the default config for JSON-RPC, GRPC (ports) and Networking
func DefaultConfig() *Config {
	return &Config{
		JSONRPCAddr:    &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: types.DefaultJSONRPCPort},
		GRPCAddr:       &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: types.DefaultGRPCPort},
		Network:        p2p.DefaultConfig(),
		Telemetry:      &Telemetry{PrometheusAddr: nil},
		SecretsManager: nil,
		BlockTime:      types.DefaultBlockTime,
	}
}

// Telemetry holds the config details for metric services
type Telemetry struct {
	PrometheusAddr *net.TCPAddr
}