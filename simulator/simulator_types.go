package simulator

import (
	cosmosTypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/osmosis-labs/osmosis/v13/x/gamm/types"
)

// Results of the simulation
// swagger:model SimulatedSwapResult
type SimulatedSwapResult struct {
	// the user's swap including the most efficient routes (pools) to use
	SimulatedUserSwap *SimulatedSwap `json:"userSwap,omitempty"`
	// how much arbitrage the user's swap will cause, routes to use, etc
	ArbitrageSwap *ArbitrageSwap `json:"arbitrageSwap,omitempty"`
	// Whether or not the user's swap will cause arbitrage once executed on chain
	HasArbitrageOpportunity bool
	// if there was some issue detected on the server
	Error string `json:"error,omitempty"`
	// Address of the user whose tokens will be swapped
	UserAddress string //e.g. osmo14tkd4079rnk7vnt0q9pg3pj44eyz8ahqrtajln
}

type ArbitrageSwap struct {
	// the arbitrage swap including the most efficient routes (pools) to use
	SimulatedSwap *SimulatedSwap
	// e.g. 165.1269 OSMO
	//
	// example: 165.1269 OSMO
	EstimatedProfitHumanReadable string
	// e.g. 165.1269
	//
	// example: 165.1269
	EstimatedProfitBaseAmount string
}

type SimulatedSwap struct {
	// Will be the exact amount/denomination to submit on-chain for the trade
	TokenIn cosmosTypes.Coin
	// Will be the exact amount to submit on-chain as the minimum amount out for the trade
	TokenOutMinAmount cosmosTypes.Int
	//Comma separated list of pools that will be traded through (only for human readable info)
	Pools string
	// The exact routes to use for the trade. These are the gamm routes used by Osmosis DEX.
	// example: [{"pool_id":1,"token_out_denom":"uosmo"}]
	Routes types.SwapAmountInRoutes `json:"routes,omitempty"`
	// Will be the simulated amount that will be received when this trade is submitted to the chain.
	TokenOutAmount cosmosTypes.Int
	// One of the 'denom' from asset lists at https:// github.com/osmosis-labs/assetlists/tree/main/osmosis-1
	TokenOutDenom string
	// One of the 'symbol' from asset lists at https:// github.com/osmosis-labs/assetlists/tree/main/osmosis-1
	TokenInSymbol string
	// example: 165.1269 OSMO
	AmountOutHumanReadable string
	// One of the 'symbol' from asset lists at https:// github.com/osmosis-labs/assetlists/tree/main/osmosis-1
	TokenOutSymbol string
	// example: 165.1269
	BaseAmount string
	// Amount this trade impacts the pool prices. For example, .025 would mean a 2.5% impact.
	// example: .025
	PriceImpact float64
}
