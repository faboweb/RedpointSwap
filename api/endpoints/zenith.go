package endpoints

import (
	"net/http"
	"time"

	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/DefiantLabs/RedpointSwap/zenith"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func SwapZenith(context *gin.Context) {
	start := time.Now()
	end := time.Now()
	config.Logger.Info("Swap Zenith", zap.Duration("time (milliseconds)", end.Sub(start)/time.Millisecond))
	context.JSON(http.StatusOK, "")
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
