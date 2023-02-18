package osmosis

import (
	"errors"
	"fmt"

	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gammTypes "github.com/osmosis-labs/osmosis/v13/x/gamm/types"
	"go.uber.org/zap"
)

func GetSignedTx(
	txClient client.Context,
	msgs []sdk.Msg,
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

func BuildArbitrageSwap(txClient client.Context, tokenIn sdk.Coin, routes gammTypes.SwapAmountInRoutes) ([]sdk.Msg, error) {
	arbs := []sdk.Msg{}
	amountRemaining := tokenIn.Amount
	totalMsgs := 0
	arbWalletBalance := config.HotWalletArbBalance

	if len(routes) == 0 {
		return nil, errors.New("no arbitrage routes in request")
	} else if routes[len(routes)-1].TokenOutDenom != tokenIn.Denom { //Verify that the token denom in matches the last route's denom out (arb trade)
		lastRouteOutDenom := routes[len(routes)-1].TokenOutDenom
		config.Logger.Error("Invalid arbitrage trade",
			zap.String("token in", tokenIn.String()),
			zap.String("last route out denom", lastRouteOutDenom),
		)
		return nil, fmt.Errorf("invalid arbitrage trade, token in %s does not match denom out %s", tokenIn.String(), lastRouteOutDenom)
	}

	for amountRemaining.GT(sdk.ZeroInt()) && totalMsgs < 25 {
		tokenIn := sdk.NewCoin(tokenIn.Denom, amountRemaining)
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

func EstimateArbGas(tokenIn sdk.Coin, routes gammTypes.SwapAmountInRoutes) (uint64, error) {
	amountRemaining := tokenIn.Amount
	totalMsgs := 0
	arbWalletBalance := config.HotWalletArbBalance

	if len(routes) == 0 {
		return 0, errors.New("no arbitrage routes in request")
	} else if routes[len(routes)-1].TokenOutDenom != tokenIn.Denom { //Verify that the token denom in matches the last route's denom out (arb trade)
		lastRouteOutDenom := routes[len(routes)-1].TokenOutDenom
		config.Logger.Error("Invalid arbitrage trade",
			zap.String("token in", tokenIn.String()),
			zap.String("last route out denom", lastRouteOutDenom),
		)
		return 0, fmt.Errorf("invalid arbitrage trade, token in %s does not match denom out %s", tokenIn.String(), lastRouteOutDenom)
	}

	for amountRemaining.GT(sdk.ZeroInt()) && totalMsgs < 25 {
		tokenIn := sdk.NewCoin(tokenIn.Denom, amountRemaining)
		if amountRemaining.GT(arbWalletBalance) {
			tokenIn.Amount = arbWalletBalance
		}

		amountRemaining = amountRemaining.Sub(tokenIn.Amount)
		totalMsgs += 1
	}
	gasFee := GetGasFee(len(routes) * totalMsgs)
	return gasFee, nil
}
