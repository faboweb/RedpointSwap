package endpoints

import (
	"fmt"
	"net/http"
	"time"

	"github.com/DefiantLabs/RedpointSwap/api"
	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/DefiantLabs/RedpointSwap/osmosis"
	"github.com/cosmos/cosmos-sdk/client"
	ctypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/gin-gonic/gin"
	gamm "github.com/osmosis-labs/osmosis/v13/x/gamm/types"
	"go.uber.org/zap"
)

func SwapAuthz(context *gin.Context) {
	start := time.Now()
	conf := config.Conf

	var request api.SimulatedSwapResult
	if err := context.ShouldBindJSON(&request); err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		context.Abort()
		return
	}

	claims, ok := context.Get("x-claims-validated")
	if !ok {
		context.JSON(http.StatusBadRequest, gin.H{"error": "user address cannot be found, no jwt provided"})
		context.Abort()
		return
	}

	jwtClaims := claims.(*api.JWTClaim)
	jwtUserAddress := jwtClaims.Subject

	//Verify the JWT contains a cosmos address (the user who authorized us to submit authz swaps)
	if request.UserAddress != jwtUserAddress || !osmosis.IsValidCosmosAddress(jwtUserAddress) {
		context.JSON(http.StatusBadRequest, gin.H{"error": "invalid jwt provided"})
		context.Abort()
		return
	}

	txClient, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.GetApiRpcSubmitTxEndpoint(), conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
	if err != nil {
		config.Logger.Error("GetOsmosisTxClient", zap.Error(err))
		context.JSON(http.StatusInternalServerError, "server misconfiguration (query client error), please notify administrator")
		return
	}

	//Get user token balances
	userBalances, err := osmosis.GetAccountBalances(txClient, jwtUserAddress)
	if err != nil {
		config.Logger.Error("Failed to look up user account balances", zap.Error(err))
		context.JSON(http.StatusInternalServerError, "Internal RPC query failed, retry later")
		return
	}

	// Make sure the user's wallet has the requisite funds to do the swap
	balanceOk := osmosis.HasTokens(request.SimulatedUserSwap.TokenIn, userBalances)
	if !balanceOk {
		config.Logger.Info("Insufficient balance",
			zap.String("user address", jwtUserAddress),
			zap.String("token in", request.SimulatedUserSwap.TokenIn.String()),
		)
		context.JSON(http.StatusBadRequest, "Insufficient balance")
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

	res, txB, err := submitTx(txClient, msgs, gas)
	if err != nil && res == nil {
		context.JSON(http.StatusBadRequest, "failed to submit trade via RPC")
		return
	} else if err != nil && res != nil {
		context.JSON(http.StatusBadRequest, fmt.Sprintf("trade with hash %s submitted to node, but failed", res.TxHash))
		return
	}

	id, err := api.AddTxSet([][]byte{txB}, &request, txClient.TxConfig.TxDecoder(), "Zenith", request.UserAddress, config.HotWalletAddress)
	if err != nil {
		fmt.Println("Tracking info may be unavailable for TX set due to unexpected error " + err.Error())
	}

	end := time.Now()
	config.Logger.Info("Swap Authz", zap.Duration("time (milliseconds)", end.Sub(start)/time.Millisecond))
	context.JSON(http.StatusOK, gin.H{"id": id})
}

func buildUserSwap(simulatedUserSwap *api.SimulatedSwap, address string) types.Msg {
	tokenIn := simulatedUserSwap.TokenIn
	tokenOutMinAmt := simulatedUserSwap.TokenOutMinAmount
	routes := simulatedUserSwap.Routes

	fmt.Printf("Authz requested with user swap: Token in: %s. Minimum amount out: %s. Pool(s) %s.\n",
		tokenIn,
		tokenOutMinAmt,
		simulatedUserSwap.Pools)

	return osmosis.BuildSwapExactAmountIn(tokenIn, tokenOutMinAmt, routes, address)
}

func getGasFee(numRoutes int) uint64 {
	return uint64(numRoutes * 200000)
}

func submitTx(
	txClient client.Context,
	msgs []types.Msg,
	txGas uint64,
) (*types.TxResponse, []byte, error) {
	txBytes, err := osmosis.GetSignedTx(txClient, msgs, txGas)
	if err != nil {
		return nil, nil, err
	}
	r, e := txClient.BroadcastTxSync(txBytes)
	return r, txBytes, e
}

func buildSwaps(
	txClient client.Context,
	swapRequest api.SimulatedSwapResult,
) (msgs []types.Msg, gasNeeded uint64, err error) {
	msgs = []types.Msg{}
	msgUserSwap := buildUserSwap(swapRequest.SimulatedUserSwap, swapRequest.UserAddress)

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
		fmt.Printf("Authz requested with arbitrage swap: Token in: %s. Pool(s) %s.\n",
			swapRequest.ArbitrageSwap.SimulatedSwap.TokenIn.String(), swapRequest.ArbitrageSwap.SimulatedSwap.Pools)

		arbSwaps, err := osmosis.BuildArbitrageSwap(txClient, swapRequest.ArbitrageSwap.SimulatedSwap.TokenIn, swapRequest.ArbitrageSwap.SimulatedSwap.Routes)
		if err != nil {
			return nil, 0, err
		}
		msgs = append(msgs, arbSwaps...)
		gasNeeded = gasNeeded + getGasFee(len(swapRequest.ArbitrageSwap.SimulatedSwap.Routes))
	}

	return
}
