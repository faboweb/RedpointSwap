package endpoints

import (
	"net/http"
	"time"

	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func SwapZenith(context *gin.Context) {
	start := time.Now()
	end := time.Now()
	config.Logger.Info("Swap Zenith", zap.Duration("time (milliseconds)", end.Sub(start)/time.Millisecond))
	context.JSON(http.StatusOK, "")
}
