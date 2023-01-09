package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/DefiantLabs/RedpointSwap/config"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var connectionTracker = sync.Map{}
var maxAllowedRequests = 20

type requestcounter struct {
	unverifiedRequests int
	totalRequests      int
	lastRequest        time.Time
}

type ClientAuthorization int

const (
	InfrequentIP ClientAuthorization = iota
	UnauthorizedTooManyRequests
)

func GetClientAuthorizationLevel(clientIP string) (ClientAuthorization, string) {
	//Unlimited requests are allowed in development mode
	if !config.Conf.Api.Production {
		return InfrequentIP, ""
	}

	//Track requests coming from this IP and block if it is too high.
	requests, seenBefore := connectionTracker.LoadOrStore(clientIP, &requestcounter{unverifiedRequests: 1, totalRequests: 1, lastRequest: time.Now()})
	if !seenBefore {
		config.Logger.Info("Authorized IP", zap.String("ip", clientIP), zap.Int("times seen", 1))
		return InfrequentIP, ""
	} else {
		requestCount := requests.(*requestcounter)
		requestCount.unverifiedRequests = requestCount.unverifiedRequests + 1
		requestCount.totalRequests = requestCount.totalRequests + 1

		if requestCount.unverifiedRequests < maxAllowedRequests {
			requestCount.lastRequest = time.Now()
			config.Logger.Info("Authorized IP", zap.String("ip", clientIP), zap.Int("times seen", requestCount.unverifiedRequests))
			return InfrequentIP, ""
		} else if requestCount.unverifiedRequests >= maxAllowedRequests && time.Since(requestCount.lastRequest).Minutes() >= 60.0 {
			requestCount.unverifiedRequests = 1
			requestCount.lastRequest = time.Now()
			config.Logger.Info("Authorized IP - reset backoff timer", zap.String("ip", clientIP))
			return InfrequentIP, ""
		} else {
			minutesToWait := int(10.0 - time.Since(requestCount.lastRequest).Minutes())
			config.Logger.Info("Blocked IP (backoff)", zap.String("ip", clientIP), zap.Int("minutes to wait", minutesToWait))
			return UnauthorizedTooManyRequests, strconv.FormatInt(int64(minutesToWait), 10)
		}
	}
}

func PreAuth() gin.HandlerFunc {
	return func(context *gin.Context) {
		if !Initialized {
			config.Logger.Error("App is not initialized")
			context.JSON(http.StatusInternalServerError, "app is not initialized, contact the system administrator (info@defiantlabs.net) if this error persists")
			return
		}

		clientIP := context.ClientIP()
		authLevel, secondaryInfo := GetClientAuthorizationLevel(clientIP)
		if authLevel == UnauthorizedTooManyRequests {
			context.JSON(http.StatusTooManyRequests, gin.H{"error": fmt.Sprintf("Too many requests. Try again in %s minutes", secondaryInfo)})
			context.Abort()
			return
		}

		config.Logger.Debug("PreAuth", zap.String("url", context.FullPath()))
		tokenString := context.GetHeader("Authorization")
		if tokenString != "" {
			claims, err := ValidateToken(tokenString)
			if err == nil {
				context.Set("x-claims-validated", claims)
			}
		}

		context.Next()
	}
}

func Auth() gin.HandlerFunc {
	return func(context *gin.Context) {
		config.Logger.Debug("Auth", zap.String("url", context.FullPath()))

		_, ok := context.Get("x-claims-validated")
		if !ok {
			tokenString := context.GetHeader("Authorization")
			if tokenString == "" {
				context.JSON(401, gin.H{"error": "request does not contain an access token"})
				context.Abort()
				return
			}
			_, err := ValidateToken(tokenString)
			if err != nil {
				context.JSON(401, gin.H{"error": err.Error()})
				context.Abort()
				return
			}
		}

		context.Next()
	}
}
