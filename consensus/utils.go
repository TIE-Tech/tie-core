package consensus

import (
	"github.com/tie-core/types"
	"github.com/tie-core/types/calcroot"
)

// BuildBlockParams are parameters passed into the BuildBlock common method
type BuildBlockParams struct {
	Header   *types.Header
	Txns     []*types.Transaction
	Receipts []*types.Receipt
}

// BuildBlock is a utility function that builds a block, based on the passed in header, transactions and receipts
func BuildBlock(params BuildBlockParams) *types.Block {
	txs := params.Txns
	header := params.Header

	if len(txs) == 0 {
		header.TxRoot = types.EmptyRootHash
	} else {
		header.TxRoot = calcroot.CalculateTransactionsRoot(txs)
	}

	if len(params.Receipts) == 0 {
		header.ReceiptsRoot = types.EmptyRootHash
	} else {
		header.ReceiptsRoot = calcroot.CalculateReceiptsRoot(params.Receipts)
	}

	header.Sha3Uncles = types.EmptyUncleHash
	header.ComputeHash()

	return &types.Block{
		Header:       header,
		Transactions: txs,
	}
}