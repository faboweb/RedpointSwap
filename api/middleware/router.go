package middleware

import (
	"strings"
	"time"

	restApi "github.com/DefiantLabs/RedpointSwap/api"
	"github.com/DefiantLabs/RedpointSwap/api/endpoints"
	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func InitializeRestApi() {
	config.Logger.Info("Logger start", zap.Time("Init() time", time.Now()))
	config.Logger.Info("CORS domains", zap.String("Allowed domains", config.Conf.Api.AllowedCORSDomains))

	port := ":80"
	if config.Conf.Api.Port != "" {
		if !strings.HasPrefix(port, ":") {
			port = ":" + config.Conf.Api.Port
		} else {
			port = config.Conf.Api.Port
		}
	}

	router := initRouter(config.Conf.Api.AllowedCORSDomains)
	restApi.Initialized = true
	err := router.Run(port)
	if err != nil {
		config.Logger.Fatal("API Run", zap.Error(err))
	}
}

func initRouter(allowedCORSDomains string) *gin.Engine {
	allowedDomains := map[string]struct{}{}
	domains := strings.Split(allowedCORSDomains, ",")
	for _, domain := range domains {
		trimmedDomain := strings.TrimSpace(domain)
		allowedDomains[trimmedDomain] = struct{}{}
	}

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())
	router.Use(CORSMiddleware(allowedDomains))
	router.SetTrustedProxies(nil)

	api := router.Group("/api")
	api.Use(PreAuth())
	secured := api.Group("/secured")
	secured.Use(Auth())

	api.GET("/status", endpoints.GetTradeStatus)                 //get status of a given in progress or completed trade
	api.GET("/zenithavailable", endpoints.ZenithAvailableBlocks) //get list of available zenith blocks
	api.GET("/grantee", endpoints.AuthzGranteeInfo)              //API endpoint so that clients know what hot wallet to authorize for grants
	api.POST("/token", endpoints.GenerateToken)

	//TODO: Consider if this should be under secured route. Bid fees are a concern.
	api.POST("/zenith", endpoints.SwapZenith)

	//Since users do NOT directly sign Authz swap requests, this endpoint is secured with a JWT to prevent abuse
	secured.POST("/authz", endpoints.SwapAuthz)
	return router
}

// Returns the origin hostname if found, or empty string otherwise.
// Only matches origins starting with http:// and https://.
func getOriginHostname(origin string) string {
	if _, hostAndPort, found := strings.Cut(origin, "http://"); found {
		host, _, _ := strings.Cut(hostAndPort, ":")
		return host
	} else if _, hostAndPort, found := strings.Cut(origin, "https://"); found {
		host, _, _ := strings.Cut(hostAndPort, ":")
		return host
	}

	return ""
}

// CORSMiddleware configures CORS for the gin router
func CORSMiddleware(allowedCORSDomains map[string]struct{}) gin.HandlerFunc {
	_, allowAllDomains := allowedCORSDomains["*"]

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		config.Logger.Debug("HTTP Request", zap.String("origin", origin))

		// All domains are allowed so we can skip parsing the hostname from the origin
		if allowAllDomains {
			// origin is present in the request, so set the CORS heads to the exact origin of the user-agent
			if origin != "" {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				config.Logger.Debug("Request from host (whitelisted)", zap.String("Access-Control-Allow-Origin", origin))
			} else { // origin not present in the request, set to the wildcard
				c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
				config.Logger.Debug("Request from host (whitelisted)", zap.String("Access-Control-Allow-Origin", origin))
			}
		} else { // Parse the hostname from the header and set as the CORS origin if it is in our whitelist
			host := getOriginHostname(origin)
			_, ok := allowedCORSDomains[host]
			if ok && host != "" {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				config.Logger.Debug("Request from host (wildcard)", zap.String("Access-Control-Allow-Origin", origin))
			}
		}
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
