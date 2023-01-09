package osmosis

import (
	cosmosSdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/osmosis-labs/osmosis/v12/x/gamm/types"
)

func BuildSwapExactAmountIn(tokenIn cosmosSdk.Coin, tokenOutMinAmt cosmosSdk.Int, routes []types.SwapAmountInRoute, address string) (cosmosSdk.Msg, error) {

	msg := &types.MsgSwapExactAmountIn{
		Sender:            address,
		Routes:            routes,
		TokenIn:           tokenIn,
		TokenOutMinAmount: tokenOutMinAmt,
	}

	return msg, nil
}
