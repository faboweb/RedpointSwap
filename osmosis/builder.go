package osmosis

import (
	"errors"
	"fmt"

	"github.com/DefiantLabs/RedpointSwap/api"
	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/types"
	"go.uber.org/zap"
)

func GetSignedTx(
	txClient client.Context,
	msgs []types.Msg,
	txGas uint64,
) ([]byte, error) {
	txf := BuildTxFactory(txClient, txGas)
	txf, txfErr := PrepareFactory(txClient, txClient.GetFromName(), txf)
	if txfErr != nil {
		return nil, txfErr
	}
	txBuilder, err := tx.BuildUnsignedTx(txf, msgs...)
	if err != nil {
		return nil, err
	}
	err = tx.Sign(txf, txClient.GetFromName(), txBuilder, true)
	if err != nil {
		return nil, err
	}

	return txClient.TxConfig.TxEncoder()(txBuilder.GetTx())
}

func BuildArbitrageSwap(txClient client.Context, simulatedArbSwap *api.SimulatedSwap) ([]types.Msg, error) {
	arbs := []types.Msg{}
	amountRemaining := simulatedArbSwap.TokenIn.Amount
	totalMsgs := 0
	arbWalletBalance := api.HotWalletArbBalance
	routes := simulatedArbSwap.Routes

	if len(routes) == 0 {
		return nil, errors.New("no arbitrage routes in request")
	} else if routes[len(routes)-1].TokenOutDenom != simulatedArbSwap.TokenIn.Denom { //Verify that the token denom in matches the last route's denom out (arb trade)
		lastRouteOutDenom := routes[len(routes)-1].TokenOutDenom
		config.Logger.Error("Invalid arbitrage trade",
			zap.String("token in", simulatedArbSwap.TokenIn.String()),
			zap.String("last route out denom", lastRouteOutDenom),
		)
		return nil, fmt.Errorf("invalid arbitrage trade, token in %s does not match denom out %s", simulatedArbSwap.TokenIn.String(), lastRouteOutDenom)
	}

	for amountRemaining.GT(types.ZeroInt()) && totalMsgs < 25 {
		tokenIn := types.NewCoin(simulatedArbSwap.TokenIn.Denom, amountRemaining)
		if amountRemaining.GT(arbWalletBalance) {
			tokenIn.Amount = arbWalletBalance
		}

		amountRemaining = amountRemaining.Sub(tokenIn.Amount)

		//Note that the minimum amount out is the same as token in. This prevents swaps where the hot wallet loses funds (excluding fees)
		tokenOutMinAmt := tokenIn.Amount
		arbs = append(arbs, BuildSwapExactAmountIn(tokenIn, tokenOutMinAmt, routes, txClient.GetFromAddress().String()))
		totalMsgs += 1
	}

	return arbs, nil
}
