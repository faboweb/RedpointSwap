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

	bidTxs, txBytes, err := zenith.PlaceBid(req, txClient)
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
