package types

import "math/big"

const (
	WEI  = int64(1e18)
	GWEI = int64(1e9)

	DefaultEpochSize = 43200
	OneYearEpoch     = 438000

	DefaultGRPCPort    int = 7749
	DefaultJSONRPCPort int = 8545
	DefaultLibp2pPort  int = 6636
	DefaultBlockTime       = 2 // in seconds

	RewardPool = "0xC79543f253dBf1F7606499be536620c1B1358e1C"
	TxFeePool  = "0x89055606E4DD8F04C3014903C202AfF35691D2BA"
)

var GasCap = big.NewInt(5000000)
