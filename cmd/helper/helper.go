package helper

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	params2 "github.com/TIE-Tech/tie-core/params"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/TIE-Tech/tie-core/common/common"
	helperFlags "github.com/TIE-Tech/tie-core/common/flags"
	"github.com/TIE-Tech/tie-core/types"
	"github.com/mitchellh/cli"
	"github.com/ryanuber/columnize"
)

const (
	GenesisFileName       = "./genesis.json"
	DefaultChainName      = "tie"
	DefaultChainID        = 100
	DefaultPremineBalance = "0x3635C9ADC5DEA00000" // 1000 ETH
	DefaultConsensus      = "pow"
	DefaultMaxSlots       = 4096
	GenesisGasUsed        = 458752   // 0x70000
	GenesisGasLimit       = 30000000 // 0x1c9c380
)

// FlagDescriptor contains the description elements for a command flag
type FlagDescriptor struct {
	Description       string   // Flag description
	Arguments         []string // Arguments list
	ArgumentsOptional bool     // Flag indicating if flag arguments are optional
	FlagOptional      bool
}

// GetDescription gets the flag description
func (fd *FlagDescriptor) GetDescription() string {
	return fd.Description
}

// GetArgumentsList gets the list of arguments for the flag
func (fd *FlagDescriptor) GetArgumentsList() []string {
	return fd.Arguments
}

// AreArgumentsOptional checks if the flag arguments are optional
func (fd *FlagDescriptor) AreArgumentsOptional() bool {
	return fd.ArgumentsOptional
}

// IsFlagOptional checks if the flag itself is optional
func (fd *FlagDescriptor) IsFlagOptional() bool {
	return fd.FlagOptional
}

// GenerateHelp is a utility function called by every command's Help() method
func GenerateHelp(synopsys string, usage string, flagMap map[string]FlagDescriptor) string {
	helpOutput := ""

	flagCounter := 0

	for flagEl, descriptor := range flagMap {
		helpOutput += GenerateFlagDesc(flagEl, descriptor) + "\n"
		flagCounter++

		if flagCounter < len(flagMap) {
			helpOutput += "\n"
		}
	}

	if len(flagMap) > 0 {
		return fmt.Sprintf("Description:\n\n%s\n\nUsage:\n\n\t%s\n\nFlags:\n\n%s", synopsys, usage, helpOutput)
	} else {
		return fmt.Sprintf("Description:\n\n%s\n\nUsage:\n\n\t%s\n", synopsys, usage)
	}
}

// GenerateFlagDesc generates the flag descriptions in a readable format
func GenerateFlagDesc(flagEl string, descriptor FlagDescriptor) string {
	// Generate the top row (with various flags)
	topRow := fmt.Sprintf("--%s", flagEl)

	argumentsOptional := descriptor.AreArgumentsOptional()
	argumentsList := descriptor.GetArgumentsList()

	argLength := len(argumentsList)

	if argLength > 0 {
		topRow += " "
		if argumentsOptional {
			topRow += "["
		}

		for argIndx, argument := range argumentsList {
			topRow += argument

			if argIndx < argLength-1 && argLength > 1 {
				topRow += " "
			}
		}

		if argumentsOptional {
			topRow += "]"
		}
	}

	// Generate the bottom description
	bottomRow := fmt.Sprintf("\t%s", descriptor.GetDescription())

	return fmt.Sprintf("%s\n%s", topRow, bottomRow)
}

// GenerateUsage is a common function for generating cmd usage text
func GenerateUsage(baseCommand string, flagMap map[string]FlagDescriptor) string {
	output := baseCommand + " "

	maxFlagsPerLine := 3 // Just an arbitrary value, can be anything reasonable

	var addedFlags int // Keeps track of when a newline character needs to be inserted

	for flagEl, descriptor := range flagMap {
		// Open the flag bracket
		if descriptor.IsFlagOptional() {
			output += "["
		}

		// Add the actual flag name
		output += fmt.Sprintf("--%s", flagEl)

		// Open the argument bracket
		if descriptor.AreArgumentsOptional() {
			output += " ["
		}

		argumentsList := descriptor.GetArgumentsList()

		// Add the flag arguments list
		for argIndex, argument := range argumentsList {
			if argIndex == 0 && !descriptor.AreArgumentsOptional() {
				// Only called for the first argument
				output += " "
			}

			output += argument

			if argIndex < len(argumentsList)-1 {
				output += " "
			}
		}

		// Close the argument bracket
		if descriptor.AreArgumentsOptional() {
			output += "]"
		}

		// Close the flag bracket
		if descriptor.IsFlagOptional() {
			output += "]"
		}

		addedFlags++
		if addedFlags%maxFlagsPerLine == 0 {
			output += "\n\t"
		} else {
			output += " "
		}
	}

	return output
}

// HandleSignals is a common method for handling signals sent to the console
// Like stop, error, etc.
func HandleSignals(closeFn func(), ui cli.Ui) int {
	signalCh := common.GetTerminationSignalCh()
	sig := <-signalCh

	output := fmt.Sprintf("\n[SIGNAL] Caught signal: %v\n", sig)
	output += "Gracefully shutting down client...\n"

	ui.Output(output)

	// Call the Minimal server close callback
	gracefulCh := make(chan struct{})

	go func() {
		if closeFn != nil {
			closeFn()
		}

		close(gracefulCh)
	}()

	select {
	case <-signalCh:
		return 1
	case <-time.After(5 * time.Second):
		return 1
	case <-gracefulCh:
		return 0
	}
}

const (
	StatError   = "StatError"
	ExistsError = "ExistsError"
)

// GenesisGenError is a specific error type for generating genesis
type GenesisGenError struct {
	message   string
	errorType string
}

// GetMessage returns the message of the genesis generation error
func (g *GenesisGenError) GetMessage() string {
	return g.message
}

// GetType returns the type of the genesis generation error
func (g *GenesisGenError) GetType() string {
	return g.errorType
}

// VerifyGenesisExistence checks if the genesis file at the specified path is present
func VerifyGenesisExistence(genesisPath string) *GenesisGenError {
	_, err := os.Stat(genesisPath)
	if err != nil && !os.IsNotExist(err) {
		return &GenesisGenError{
			message:   fmt.Sprintf("failed to stat (%s): %v", genesisPath, err),
			errorType: StatError,
		}
	}

	if !os.IsNotExist(err) {
		return &GenesisGenError{
			message:   fmt.Sprintf("genesis file at path (%s) already exists", genesisPath),
			errorType: ExistsError,
		}
	}

	return nil
}

// FillPremineMap fills the premine map for the genesis.json file with passed in balances and accounts
func FillPremineMap(
	premineMap map[types.Address]*params2.GenesisAccount,
	premine helperFlags.ArrayFlags,
) error {
	for _, prem := range premine {
		var addr types.Address

		val := DefaultPremineBalance

		if indx := strings.Index(prem, ":"); indx != -1 {
			// <addr>:<balance>
			addr, val = types.StringToAddress(prem[:indx]), prem[indx+1:]
		} else {
			// <addr>
			addr = types.StringToAddress(prem)
		}

		amount, err := types.ParseUint256orHex(&val)
		if err != nil {
			return fmt.Errorf("failed to parse amount %s: %w", val, err)
		}

		premineMap[addr] = &params2.GenesisAccount{
			Balance: amount,
		}
	}

	return nil
}

// MergeMaps is a common method for merging multiple maps.
// If two or more maps have the same keys, the map that is passed in later
// will have its key value override the previous same key values
func MergeMaps(maps ...map[string]interface{}) map[string]interface{} {
	mergedMap := make(map[string]interface{})

	for _, m := range maps {
		for key, value := range m {
			mergedMap[key] = value
		}
	}

	return mergedMap
}

// WriteGenesisToDisk writes the passed in configuration to a genesis.json file at the specified path
func WriteGenesisToDisk(chain *params2.Chain, genesisPath string) error {
	data, err := json.MarshalIndent(chain, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to generate genesis: %w", err)
	}

	//nolint: gosec
	if err := ioutil.WriteFile(genesisPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write genesis: %w", err)
	}

	return nil
}

func ReadConfig(baseCommand string, args []string) (*Config, error) {
	config := DefaultConfig()

	cliConfig := &Config{
		Network:   &Network{},
		TxPool:    &TxPool{},
		Telemetry: &Telemetry{},
	}

	flags := flag.NewFlagSet(baseCommand, flag.ContinueOnError)
	flags.Usage = func() {}

	var configFile string

	flags.StringVar(&cliConfig.LogLevel, "log-level", "", "")
	flags.BoolVar(&cliConfig.Seal, "seal", false, "")
	flags.StringVar(&configFile, "config", "", "")
	flags.StringVar(&cliConfig.Chain, "chain", "", "")
	flags.StringVar(&cliConfig.DataDir, "data-dir", "", "")
	flags.StringVar(&cliConfig.GRPCAddr, "grpc", "", "")
	flags.StringVar(&cliConfig.JSONRPCAddr, "jsonrpc", "", "")
	flags.StringVar(&cliConfig.Join, "join", "", "")
	flags.StringVar(&cliConfig.Network.Addr, "libp2p", "", "")
	flags.StringVar(&cliConfig.Telemetry.PrometheusAddr, "prometheus", "", "")
	flags.StringVar(
		&cliConfig.Network.NatAddr,
		"nat",
		"",
		"the external IP address without port, as can be seen by peers",
	)
	flags.StringVar(
		&cliConfig.Network.DNS,
		"dns",
		"",
		" the host DNS address which can be used by a remote peer for connection",
	)
	flags.BoolVar(&cliConfig.Network.NoDiscover, "no-discover", false, "")
	flags.Int64Var(&cliConfig.Network.MaxPeers, "max-peers", -1, "maximum number of peers")
	flags.Int64Var(&cliConfig.Network.MaxInboundPeers, "max-inbound-peers", -1, "maximum number of inbound peers")
	flags.Int64Var(&cliConfig.Network.MaxOutboundPeers, "max-outbound-peers", -1, "maximum number of outbound peers")
	flags.Uint64Var(&cliConfig.TxPool.PriceLimit, "price-limit", 0, "")
	flags.Uint64Var(&cliConfig.TxPool.MaxSlots, "max-slots", DefaultMaxSlots, "")
	flags.StringVar(&cliConfig.BlockGasTarget, "block-gas-target", strconv.FormatUint(0, 10), "")
	flags.StringVar(&cliConfig.Secrets, "secrets-config", "", "")
	flags.StringVar(&cliConfig.RestoreFile, "restore", "", "")
	flags.Uint64Var(&cliConfig.BlockTime, "block-time", config.BlockTime, "")
	flags.BoolVar(&cliConfig.EsOpen, "es-open", false, "")
	flags.StringVar(&cliConfig.EsAddr, "es-addr", "", "")
	flags.StringVar(&cliConfig.EsIndex, "es-index", "tie-logs", "")
	flags.StringVar(&cliConfig.EsOwner, "es-owner", "tie-node", "")

	if err := flags.Parse(args); err != nil {
		return nil, err
	}

	if cliConfig.Network.MaxPeers != -1 {
		if cliConfig.Network.MaxInboundPeers != -1 || cliConfig.Network.MaxOutboundPeers != -1 {
			return nil, errors.New("both max-peers and max-inbound/outbound flags are set")
		}
	}

	if configFile != "" {
		// A config file has been passed in, parse it
		diskConfigFile, err := readConfigFile(configFile)
		if err != nil {
			return nil, err
		}

		if diskConfigFile.Network.MaxPeers != -1 {
			if diskConfigFile.Network.MaxInboundPeers != -1 || diskConfigFile.Network.MaxOutboundPeers != -1 {
				return nil, errors.New("both max-peers & max-inbound/outbound flags are set")
			}
		}

		if err := config.mergeConfigWith(diskConfigFile); err != nil {
			return nil, err
		}
	}

	if err := config.mergeConfigWith(cliConfig); err != nil {
		return nil, err
	}

	return config, nil
}

type HelpGenerator interface {
	DefineFlags()
}

// OUTPUT FORMATTING //

// FormatList formats a list, using a specific blank value replacement
func FormatList(in []string) string {
	columnConf := columnize.DefaultConfig()
	columnConf.Empty = "<none>"

	return columnize.Format(in, columnConf)
}

// FormatKV formats key value pairs:
//
// Key = Value
//
// Key = <none>
func FormatKV(in []string) string {
	columnConf := columnize.DefaultConfig()
	columnConf.Empty = "<none>"
	columnConf.Glue = " = "

	return columnize.Format(in, columnConf)
}
