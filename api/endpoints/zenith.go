package endpoints

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/DefiantLabs/RedpointSwap/api"
	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/DefiantLabs/RedpointSwap/osmosis"
	"github.com/DefiantLabs/RedpointSwap/zenith"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Submit zenith requests to a Queue and wait for an available zenith block
func QueueZenith(context *gin.Context) {
	var req zenith.QueuedBidRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	reqExpiration, err := time.Parse(time.RFC3339, req.Expiration)
	if err != nil {
		context.JSON(http.StatusBadRequest, "expiration is unrecognized format, expected RFC3339")
		return
	}

	if reqExpiration.Before(time.Now()) {
		context.JSON(http.StatusBadRequest, "expiration must be in the future")
		return
	}

	//Verify the cosmos address in the simulation
	if !osmosis.IsValidCosmosAddress(req.SimulatedSwap.UserAddress) {
		context.JSON(http.StatusBadRequest, gin.H{"error": "invalid simulation provided"})
		context.Abort()
		return
	}

	conf := config.Conf
	txClient, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.GetApiRpcSearchTxEndpoint(), conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
	if err != nil {
		config.Logger.Error("GetOsmosisTxClient", zap.Error(err))
		context.JSON(http.StatusInternalServerError, "server misconfiguration (query client error), please notify administrator")
		return
	}

	//Get user token balances
	userBalances, err := osmosis.GetAccountBalances(txClient, req.SimulatedSwap.UserAddress)
	if err != nil {
		config.Logger.Error("Failed to look up user account balances", zap.Error(err))
		context.JSON(http.StatusInternalServerError, "Internal RPC query failed, retry later")
		return
	}

	// Make sure the user's wallet has the requisite funds to do the swap
	balanceOk := osmosis.HasTokens(req.SimulatedSwap.SimulatedUserSwap.TokenIn, userBalances)
	if !balanceOk {
		config.Logger.Info("Insufficient balance",
			zap.String("user address", req.SimulatedSwap.UserAddress),
			zap.String("token in", req.SimulatedSwap.SimulatedUserSwap.TokenIn.String()),
		)
		context.JSON(http.StatusBadRequest, "Insufficient balance")
		return
	}

	api.QueueZenithRequest(req)
	context.JSON(http.StatusOK, gin.H{"status": "Queued Zenith request", "id": req.SimulatedSwap.UserAddress})
}

func SwapZenith(context *gin.Context) {
	start := time.Now()
	var req zenith.BidRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	conf := config.Conf
	txClient, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.GetApiRpcSubmitTxEndpoint(), conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
	if err != nil {
		context.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	bidTxs, txBytes, err := zenith.CreateBidParams(req, txClient)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	bidReq := &zenith.ZenithBidRequest{
		ChainID: req.ChainID,
		Height:  req.Height,
		Kind:    req.Kind,
		Txs:     bidTxs,
	}

	reqBytes, err := json.Marshal(bidReq)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"error": "failed to marshal request to zenith api"})
		return
	}

	//Send the request to the Zenith API
	httpReq, err := http.NewRequest("POST", config.Conf.Zenith.ZenithBidUrl, bytes.NewBuffer(reqBytes))
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"error": "issue creating http request for zenith"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		context.JSON(resp.StatusCode, gin.H{"error": "failed to send request to zenith api"})
		return
	}
	defer resp.Body.Close()

	var bidResponse zenith.BidResponse
	err = json.NewDecoder(resp.Body).Decode(&bidResponse)
	if err != nil {
		fmt.Println("failed to decode response from zenith api")
	} else {
		id, err := api.AddTxSet(txBytes, &req.SimulatedSwap, txClient.TxConfig.TxDecoder(), "Zenith", req.SimulatedSwap.UserAddress, config.HotWalletAddress)
		if err != nil {
			fmt.Println("Tracking info may be unavailable for TX set due to unexpected error " + err.Error())
		} else {
			bidResponse.Id = id
		}
	}

	end := time.Now()
	config.Logger.Info("Swap Zenith", zap.Duration("time (milliseconds)", end.Sub(start)/time.Millisecond))
	context.JSON(resp.StatusCode, bidResponse)
}

func ZenithAvailableBlocks(context *gin.Context) {
	zBlocks := zenith.GetZenithBlocks()

	if blocksAfter, ok := context.GetQuery("after"); ok {
		zBlocksTimeFiltered := []*zenith.FutureBlock{}

		zenithBlocksTime, err := time.Parse(time.RFC3339, blocksAfter)
		if err != nil {
			context.JSON(http.StatusBadRequest, "zenith time filter provided, but unrecognized format")
			return
		}

		for _, block := range zBlocks {
			if block.ProjectedBlocktime.After(zenithBlocksTime) {
				zBlocksTimeFiltered = append(zBlocksTimeFiltered, block)
			}
		}

		context.JSON(http.StatusOK, zBlocksTimeFiltered)
	} else {
		context.JSON(http.StatusOK, zBlocks)
	}
}
