package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/TIE-Tech/tie-core/common/crypto"
	"github.com/TIE-Tech/tie-core/core/nodekey"
	"github.com/TIE-Tech/tie-core/core/nodekey/hashicorpvault"
	"github.com/TIE-Tech/tie-core/core/nodekey/local"
	"path/filepath"

	"github.com/TIE-Tech/tie-core/cmd/helper"
	"github.com/TIE-Tech/tie-core/common/common"
	"github.com/TIE-Tech/tie-core/p2p"
	"github.com/TIE-Tech/tie-core/types"
	"github.com/libp2p/go-libp2p-core/peer"
)

// SecretsInit is the command to query the snapshot
type SecretsInit struct {
	helper.Base
	Formatter *helper.FormatterFlag
}

var (
	ErrInvalidSecretsConfig = errors.New("invalid secrets configuration file")
)

func (p *SecretsInit) DefineFlags() {
	p.Base.DefineFlags(p.Formatter)

	p.FlagMap["data-dir"] = helper.FlagDescriptor{
		Description: "Sets the directory for the TIE data if the local FS is used",
		Arguments: []string{
			"DATA_DIRECTORY",
		},
		ArgumentsOptional: false,
		FlagOptional:      true,
	}

	p.FlagMap["config"] = helper.FlagDescriptor{
		Description: "Sets the path to the SecretsManager config file. Used for Hashicorp Vault. " +
			"If omitted, the local FS secrets manager is used",
		Arguments: []string{
			"SECRETS_CONFIG",
		},
		ArgumentsOptional: false,
		FlagOptional:      true,
	}
}

// GetHelperText returns a simple description of the command
func (p *SecretsInit) GetHelperText() string {
	return "Initializes private keys for the TIE (Validator + Networking) to the specified Secrets Manager"
}

// Help implements the cli.SecretsInit interface
func (p *SecretsInit) Help() string {
	p.DefineFlags()

	return helper.GenerateHelp(p.Synopsis(), helper.GenerateUsage(p.GetBaseCommand(), p.FlagMap), p.FlagMap)
}

// Synopsis implements the cli.SecretsInit interface
func (p *SecretsInit) Synopsis() string {
	return p.GetHelperText()
}

func (p *SecretsInit) GetBaseCommand() string {
	return "secrets init"
}

// generateAlreadyInitializedError generates an output for when the secrets directory
// has already been initialized in the past
func generateAlreadyInitializedError(directory string) string {
	return fmt.Sprintf("Directory %s has previously initialized secrets data", directory)
}

// setupLocalSM is a common method for boilerplate local secrets manager setup
func setupLocalSM(dataDir string) (nodekey.SecretsManager, error) {
	subDirectories := []string{nodekey.ConsensusFolderLocal, nodekey.NetworkFolderLocal}

	// Check if the sub-directories exist / are already populated
	for _, subDirectory := range subDirectories {
		if common.DirectoryExists(filepath.Join(dataDir, subDirectory)) {
			return nil, errors.New(generateAlreadyInitializedError(dataDir))
		}
	}

	return local.SecretsManagerFactory(
		nil, // Local secrets manager doesn't require a config
		&nodekey.SecretsManagerParams{
			Extra: map[string]interface{}{
				nodekey.Path: dataDir,
			},
		})
}

// setupHashicorpVault is a common method for boilerplate hashicorp vault secrets manager setup
func setupHashicorpVault(
	secretsConfig *nodekey.SecretsManagerConfig,
) (nodekey.SecretsManager, error) {
	return hashicorpvault.SecretsManagerFactory(
		secretsConfig,
		&nodekey.SecretsManagerParams{},
	)
}

// Run implements the cli.SecretsInit interface
func (p *SecretsInit) Run(args []string) int {
	flags := p.Base.NewFlagSet(p.GetBaseCommand(), p.Formatter)

	var dataDir string

	var configPath string

	flags.StringVar(&dataDir, "data-dir", "", "")
	flags.StringVar(&configPath, "config", "", "")

	if err := flags.Parse(args); err != nil {
		p.Formatter.OutputError(err)

		return 1
	}

	if dataDir == "" && configPath == "" {
		p.Formatter.OutputError(errors.New("required argument (data directory) not passed in"))

		return 1
	}

	var secretsManager nodekey.SecretsManager

	if configPath == "" {
		// No secrets manager config specified,
		// use the local secrets manager
		localSecretsManager, setupErr := setupLocalSM(dataDir)
		if setupErr != nil {
			p.Formatter.OutputError(setupErr)

			return 1
		}

		secretsManager = localSecretsManager
	} else {
		// Config file passed in
		secretsConfig, readErr := nodekey.ReadConfig(configPath)
		if readErr != nil {
			p.Formatter.OutputError(fmt.Errorf("unable to read config file, %w", readErr))

			return 1
		}

		// Set up the corresponding secrets manager
		switch secretsConfig.Type {
		case nodekey.HashicorpVault:
			vaultSecretsManager, setupErr := setupHashicorpVault(secretsConfig)
			if setupErr != nil {
				p.Formatter.OutputError(setupErr)

				return 1
			}

			secretsManager = vaultSecretsManager
		default:
			p.Formatter.OutputError(errors.New("unknown secrets manager type"))

			return 1
		}
	}

	// Generate the IBFT validator private key
	validatorKey, validatorKeyEncoded, keyErr := crypto.GenerateAndEncodePrivateKey()
	if keyErr != nil {
		p.Formatter.OutputError(keyErr)

		return 1
	}

	// Write the validator private key to the secrets manager storage
	if setErr := secretsManager.SetSecret(nodekey.ValidatorKey, validatorKeyEncoded); setErr != nil {
		p.Formatter.OutputError(setErr)

		return 1
	}

	// Generate the libp2p private key
	libp2pKey, libp2pKeyEncoded, keyErr := p2p.GenerateAndEncodeLibp2pKey()
	if keyErr != nil {
		p.Formatter.OutputError(keyErr)

		return 1
	}

	// Write the networking private key to the secrets manager storage
	if setErr := secretsManager.SetSecret(nodekey.NetworkKey, libp2pKeyEncoded); setErr != nil {
		p.Formatter.OutputError(setErr)

		return 1
	}

	nodeID, err := peer.IDFromPrivateKey(libp2pKey)
	if err != nil {
		p.Formatter.OutputError(err)

		return 1
	}

	res := &SecretsInitResult{
		Address: crypto.PubKeyToAddress(&validatorKey.PublicKey),
		NodeID:  nodeID.String(),
	}
	p.Formatter.OutputResult(res)

	return 0
}

type SecretsInitResult struct {
	Address types.Address `json:"address"`
	NodeID  string        `json:"node_id"`
}

func (r *SecretsInitResult) Output() string {
	var buffer bytes.Buffer

	buffer.WriteString("\n[SECRETS INIT]\n")
	buffer.WriteString(helper.FormatKV([]string{
		fmt.Sprintf("Public key (address)|%s", r.Address),
		fmt.Sprintf("Node ID|%s", r.NodeID),
	}))
	buffer.WriteString("\n")

	return buffer.String()
}
