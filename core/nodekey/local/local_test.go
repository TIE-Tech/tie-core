package local

import (
	"encoding/hex"
	"github.com/tie-core/common/crypto"
	"github.com/tie-core/core/nodekey"
	"io/ioutil"
	"os"
	"testing"

	libp2pCrypto "github.com/libp2p/go-libp2p-core/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/tie-core/common/common"
)

func TestLocalSecretsManagerFactory(t *testing.T) {
	// Set up the expected folder structure
	workingDirectory, tempErr := ioutil.TempDir("/tmp", "local-secrets-manager")
	if tempErr != nil {
		t.Fatalf("Unable to instantiate local secrets manager directories, %v", tempErr)
	}

	// Set up a clean-up procedure
	t.Cleanup(func() {
		_ = os.RemoveAll(workingDirectory)
	})

	testTable := []struct {
		name          string
		config        *nodekey.SecretsManagerParams
		shouldSucceed bool
	}{
		{
			"Valid configuration with path info",
			&nodekey.SecretsManagerParams{
				Extra: map[string]interface{}{
					nodekey.Path: workingDirectory,
				},
			},
			true,
		},
		{
			"Invalid configuration without path info",
			&nodekey.SecretsManagerParams{
				Extra: map[string]interface{}{
					"dummy": 123,
				},
			},
			false,
		},
	}

	for _, testCase := range testTable {
		t.Run(testCase.name, func(t *testing.T) {
			localSecretsManager, factoryErr := SecretsManagerFactory(nil, testCase.config)
			if testCase.shouldSucceed {
				assert.NotNil(t, localSecretsManager)
				assert.NoError(t, factoryErr)
			} else {
				assert.Nil(t, localSecretsManager)
				assert.Error(t, factoryErr)
			}
		})
	}
}

// getLocalSecretsManager is a common method for creating an instance of the
// local secrets manager
func getLocalSecretsManager(t *testing.T) nodekey.SecretsManager {
	t.Helper()

	// Set up the expected folder structure
	workingDirectory, tempErr := ioutil.TempDir("/tmp", "local-secrets-manager")
	if tempErr != nil {
		t.Fatalf("Unable to instantiate local secrets manager directories, %v", tempErr)
	}

	setupErr := common.SetupDataDir(workingDirectory, []string{nodekey.ConsensusFolderLocal, nodekey.NetworkFolderLocal})
	if setupErr != nil {
		t.Fatalf("Unable to instantiate local secrets manager directories, %v", setupErr)
	}

	// Set up a clean-up procedure
	t.Cleanup(func() {
		_ = os.RemoveAll(workingDirectory)
	})

	// Set up an instance of the local secrets manager
	baseConfig := &nodekey.SecretsManagerParams{
		Extra: map[string]interface{}{
			nodekey.Path: workingDirectory,
		},
	}

	manager, factoryErr := SecretsManagerFactory(nil, baseConfig)
	if factoryErr != nil {
		t.Fatalf("Unable to instantiate local secrets manager, %v", factoryErr)
	}

	assert.NotNil(t, manager)

	return manager
}

func generateAndEncodeLibp2pKey() (libp2pCrypto.PrivKey, []byte, error) {
	priv, _, err := libp2pCrypto.GenerateKeyPair(libp2pCrypto.Secp256k1, 256)
	if err != nil {
		return nil, nil, err
	}

	buf, err := libp2pCrypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}

	return priv, []byte(hex.EncodeToString(buf)), nil
}

func TestLocalSecretsManager_GetSetSecret(
	t *testing.T,
) {
	// Set up the values used in the test table
	validatorKey, validatorKeyEncoded, genErr := crypto.GenerateAndEncodePrivateKey()
	if genErr != nil {
		t.Fatalf("Unable to generate validator private key, %v", genErr)
	}

	libp2pKey, libp2pKeyEncoded, genErr := generateAndEncodeLibp2pKey()
	if genErr != nil {
		t.Fatalf("Unable to generate networking private key, %v", genErr)
	}

	// Compare validator keys common
	compareValidatorKeys := func(manager nodekey.SecretsManager) bool {
		parsedKey, parseErr := crypto.ReadConsensusKey(manager)
		if parseErr != nil {
			t.Fatalf("unable to parse validator private key, %v", parseErr)
		}

		return validatorKey.Equal(parsedKey)
	}

	// Compare networking keys common
	compareNetworkingKeys := func(manager nodekey.SecretsManager) bool {
		secret, err := manager.GetSecret(nodekey.NetworkKey)
		if err != nil {
			t.Fatalf("unable to parse networking private key, %v", err)
		}

		buf, err := hex.DecodeString(string(secret))
		if err != nil {
			t.Fatalf("unable to parse networking private key, %v", err)
		}

		parsedKey, err := libp2pCrypto.UnmarshalPrivateKey(buf)
		if err != nil {
			t.Fatalf("unable to unmarshal networking private key, %v", err)
		}

		return libp2pKey.Equals(parsedKey)
	}

	testTable := []struct {
		name          string
		secretName    string
		secretValue   []byte
		compareFunc   func(nodekey.SecretsManager) bool
		shouldSucceed bool
	}{
		{
			"Validator key storage",
			nodekey.ValidatorKey,
			validatorKeyEncoded,
			compareValidatorKeys,
			true,
		},
		{
			"Networking key storage",
			nodekey.NetworkKey,
			libp2pKeyEncoded,
			compareNetworkingKeys,
			true,
		},
		{
			"Unsupported secret storage",
			"dummySecret",
			[]byte{1},
			func(nodekey.SecretsManager) bool {
				return true
			},
			false,
		},
	}

	for _, testCase := range testTable {
		t.Run(testCase.name, func(t *testing.T) {
			// Get an instance of the secrets manager
			manager := getLocalSecretsManager(t)

			// Set the secret
			setErr := manager.SetSecret(testCase.secretName, testCase.secretValue)

			if testCase.shouldSucceed {
				// Error checks
				assert.NoError(t, setErr)

				// Grab the secret and compare
				assert.True(t, testCase.compareFunc(manager))
			} else {
				// Make sure the set method errored out
				assert.Error(t, setErr)

				// Make sure the secret is not present
				value, getErr := manager.GetSecret(testCase.secretName)

				assert.Nil(t, value)
				assert.ErrorIs(t, getErr, nodekey.ErrSecretNotFound)
			}
		})
	}
}

func TestLocalSecretsManager_RemoveSecret(t *testing.T) {
	// Set up the values used in the test table
	_, validatorKeyEncoded, genErr := crypto.GenerateAndEncodePrivateKey()
	if genErr != nil {
		t.Fatalf("Unable to generate validator private key, %v", genErr)
	}

	// Set the secret
	manager := getLocalSecretsManager(t)
	setErr := manager.SetSecret(nodekey.ValidatorKey, validatorKeyEncoded)

	if setErr != nil {
		t.Fatalf("Unable to save validator private key, %v", setErr)
	}

	testTable := []struct {
		name          string
		secretName    string
		shouldSucceed bool
	}{
		{
			"Remove existing secret",
			nodekey.ValidatorKey,
			true,
		},
		{
			"Remove non-existing secret",
			nodekey.NetworkKey,
			false,
		},
	}

	for _, testCase := range testTable {
		t.Run(testCase.name, func(t *testing.T) {
			removeErr := manager.RemoveSecret(testCase.secretName)

			if testCase.shouldSucceed {
				// Assert that no error occurred
				assert.Nil(t, removeErr)
			} else {
				// Assert the error type
				assert.Error(t, removeErr)
			}

			// Check that the value is not present
			assert.False(t, manager.HasSecret(testCase.secretName))
		})
	}
}