package osmosis

import (
	cosmosSdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/osmosis-labs/osmosis/v13/x/gamm/types"
)

func BuildSwapExactAmountIn(tokenIn cosmosSdk.Coin, tokenOutMinAmt cosmosSdk.Int, routes []types.SwapAmountInRoute, address string) cosmosSdk.Msg {

	return &types.MsgSwapExactAmountIn{
		Sender:            address,
		Routes:            routes,
		TokenIn:           tokenIn,
		TokenOutMinAmount: tokenOutMinAmt,
	}
}
