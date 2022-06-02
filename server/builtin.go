package server

import (
	"github.com/TIE-Tech/tie-core/consensus"
	"github.com/TIE-Tech/tie-core/consensus/pvbft"
	"github.com/TIE-Tech/tie-core/core/nodekey"
	"github.com/TIE-Tech/tie-core/core/nodekey/hashicorpvault"
	"github.com/TIE-Tech/tie-core/core/nodekey/local"
)

var consensusBackends = map[string]consensus.Factory{
	"ibft": pvbft.Factory,
}

// secretsManagerBackends defines the SecretManager factories for different
// secret management solutions
var secretsManagerBackends = map[nodekey.SecretsManagerType]nodekey.SecretsManagerFactory{
	nodekey.Local:          local.SecretsManagerFactory,
	nodekey.HashicorpVault: hashicorpvault.SecretsManagerFactory,
}
