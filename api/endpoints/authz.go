package endpoints

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/DefiantLabs/RedpointSwap/api"
	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/DefiantLabs/RedpointSwap/osmosis"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	ctypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/gin-gonic/gin"
	gamm "github.com/osmosis-labs/osmosis/v13/x/gamm/types"
	"go.uber.org/zap"
)

// TODO: Validation!!! (See below...)
func SwapAuthz(context *gin.Context) {
	start := time.Now()
	conf := config.Conf

	var request api.SimulatedSwapResult
	if err := context.ShouldBindJSON(&request); err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		context.Abort()
		return
	}

	// TODO:
	//
	// Pretty hard to properly validate this request but I'm thinking we should do the following:
	//
	// 1) make sure the user's wallet has the requisite funds to do the swap.
	// 2) make sure the hot wallet has the funds to do the arb swap.
	// 3) validate that the API's caller is authorized to request swaps on behalf of the user
	//    NOTE: to do this, we'll have an API endpoint that submits the authz grant,
	//    and return a JWT once authz is granted. The JWT will be bound to the user,
	//    via a claim with the user's address in it, and the JWT will be checked by this endpoint.
	//
	// config.Logger.Debug("Validating user input (submit authz swap) request)")
	// errInvalid := ValidateUserInput(&request)
	// if errInvalid != nil {
	// 	context.JSON(http.StatusBadRequest, errInvalid.Error())
	// 	return
	// }

	txClient, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.Api.Rpc, conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
	if err != nil {
		config.Logger.Error("GetOsmosisTxClient", zap.Error(err))
		context.JSON(http.StatusBadRequest, "server misconfiguration (query client error), please notify administrator")
		return
	}

	msgs, gas, err := buildSwaps(txClient, request)
	if err != nil {
		config.Logger.Error("buildSwaps", zap.Error(err))

		//Do not give caller more info on why we rejected the swap request.
		//Requests should only come from the defiant simulator and associated clients.
		context.JSON(http.StatusBadRequest, "bad swap request provided")
		return
	}

	res, err := submitTx(txClient, msgs, gas)
	if err != nil && res == nil {
		context.JSON(http.StatusBadRequest, "failed to submit trade via RPC")
		return
	} else if err != nil && res != nil {
		context.JSON(http.StatusBadRequest, fmt.Sprintf("trade with hash %s submitted to node, but failed", res.TxHash))
		return
	}

	end := time.Now()
	config.Logger.Info("Swap Authz", zap.Duration("time (milliseconds)", end.Sub(start)/time.Millisecond))
	context.JSON(http.StatusOK, "")
}

func buildUserSwap(simulatedUserSwap *api.SimulatedSwap, address string) (types.Msg, error) {
	tokenIn := simulatedUserSwap.TokenIn
	tokenOutMinAmt := simulatedUserSwap.TokenOutMinAmount
	routes := simulatedUserSwap.Routes

	fmt.Printf("Authz requested with user swap: Token in: %s. Minimum amount out: %s. Pool(s) %s.\n",
		tokenIn,
		tokenOutMinAmt,
		simulatedUserSwap.Pools)

	return osmosis.BuildSwapExactAmountIn(tokenIn, tokenOutMinAmt, routes, address)
}

func buildArbitrageSwap(txClient client.Context, simulatedArbSwap *api.SimulatedSwap) (types.Msg, error) {
	tokenIn := simulatedArbSwap.TokenIn
	//Note that the minimum amount out is the same as token in. This prevents swaps where the hot wallet loses funds (excluding fees)
	tokenOutMinAmt := simulatedArbSwap.TokenIn.Amount
	routes := simulatedArbSwap.Routes

	if len(routes) == 0 {
		return nil, errors.New("no arbitrage routes in request")
	} else if routes[len(routes)-1].TokenOutDenom != tokenIn.Denom { //Verify that the token denom in matches the last route's denom out (arb trade)
		lastRouteOutDenom := routes[len(routes)-1].TokenOutDenom
		config.Logger.Error("Invalid arbitrage trade",
			zap.String("token in", tokenIn.String()),
			zap.String("last route out denom", lastRouteOutDenom),
		)
		return nil, fmt.Errorf("invalid arbitrage trade, token in %s does not match denom out %s", tokenIn, lastRouteOutDenom)
	}

	fmt.Printf("Authz requested with arbitrage swap: Token in: %s. Minimum amount out: %s. Pool(s) %s.\n",
		tokenIn,
		tokenOutMinAmt,
		simulatedArbSwap.Pools)

	return osmosis.BuildSwapExactAmountIn(tokenIn, tokenOutMinAmt, routes, txClient.GetFromAddress().String())
}

func getGasFee(numRoutes int) uint64 {
	return uint64(numRoutes * 200000)
}

func submitTx(
	txClient client.Context,
	msgs []types.Msg,
	txGas uint64,
) (*types.TxResponse, error) {
	txf := osmosis.BuildTxFactory(txClient, txGas)
	txf, txfErr := osmosis.PrepareFactory(txClient, txClient.GetFromName(), txf)
	if txfErr != nil {
		return nil, txfErr
	}
	txBuilder, err := tx.BuildUnsignedTx(txf, msgs...)
	if err != nil {
		return nil, err
	}
	//txBuilder.SetFeeGranter(txClient.GetFeeGranterAddress())
	err = tx.Sign(txf, txClient.GetFromName(), txBuilder, true)
	txBytes, err := txClient.TxConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return nil, err
	}
	return txClient.BroadcastTxSync(txBytes)
}

func buildSwaps(
	txClient client.Context,
	swapRequest api.SimulatedSwapResult,
) (msgs []types.Msg, gasNeeded uint64, err error) {
	msgs = []types.Msg{}
	msgUserSwap, err := buildUserSwap(swapRequest.SimulatedUserSwap, swapRequest.UserAddress)
	if err != nil {
		return nil, 0, err
	}

	swapMsg := msgUserSwap.(*gamm.MsgSwapExactAmountIn)
	userSwapMsgBytes, mErr := swapMsg.Marshal()
	if mErr != nil {
		return
	}

	//txClient should be associated with the hot wallet, so this is using the hot wallet to do a trade for the user
	msgExec := &authz.MsgExec{
		Grantee: txClient.GetFromAddress().String(),
		Msgs:    []*ctypes.Any{{TypeUrl: "/osmosis.gamm.v1beta1.MsgSwapExactAmountIn", Value: userSwapMsgBytes}},
	}

	msgs = append(msgs, msgExec)
	gasNeeded = getGasFee(len(swapRequest.SimulatedUserSwap.Routes))

	// It wouldn't make a lot of sense to use the authz request endpoint if there isn't arbitrage.
	// However, it is allowed to do so.
	if swapRequest.HasArbitrageOpportunity {
		arbSwap, err := buildArbitrageSwap(txClient, swapRequest.ArbitrageSwap.SimulatedSwap)
		if err != nil {
			return nil, 0, err
		}
		msgs = append(msgs, arbSwap)
		gasNeeded = gasNeeded + getGasFee(len(swapRequest.ArbitrageSwap.SimulatedSwap.Routes))
	}

	return
}
