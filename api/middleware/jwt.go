package middleware

import (
	b64 "encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/DefiantLabs/RedpointSwap/osmosis"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"go.uber.org/zap"
)

type TokenRequest struct {
	Address          string `json:"address"`
	Base64AuthzGrant string `json:"authz_grant"`
}

type AuthzGranteeResponse struct {
	GranteeAddress string `json:"authz_grantee"`
}

var jwtKey []byte

func SetSecretKey(jwtSecret string) {
	jwtKey = []byte(jwtSecret)
}

type JWTClaim struct {
	Address string `json:"address"`
	jwt.RegisteredClaims
}

func ValidateToken(signedToken string) (claims *JWTClaim, err error) {
	token, err := jwt.ParseWithClaims(signedToken, &JWTClaim{}, func(token *jwt.Token) (interface{}, error) {
		//validate the alg is correct
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		return jwtKey, nil
	})
	ok := false
	claims, ok = token.Claims.(*JWTClaim)
	if !ok {
		err = errors.New("couldn't parse claims")
		return
	}
	if ok && token.Valid {
		if claims.ExpiresAt.Before(time.Now()) {
			err = errors.New("token expired")
			return
		}
	} else {
		err = fmt.Errorf("token not valid")
	}

	return
}

func AuthzGranteeInfo(context *gin.Context) {
	context.JSON(http.StatusOK, &AuthzGranteeResponse{GranteeAddress: HotWalletAddress})
}

// Verifies a user's identity through a valid, signed authz grant. Note: considering cosmos-sdk/MsgVerifyInvariant instead.
func GenerateToken(context *gin.Context) {
	conf := config.Conf
	var request TokenRequest
	if err := context.ShouldBindJSON(&request); err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		context.Abort()
		return
	}

	//Once we figure out roughly how long this should be, I'll update the length to something more reasonable
	if len(request.Base64AuthzGrant) == 0 {
		context.JSON(http.StatusBadRequest, gin.H{"error": "requestor did not provide a valid, base 64 encoded authz grant"})
		context.Abort()
		return
	}

	txBytes, err := b64.StdEncoding.DecodeString(request.Base64AuthzGrant)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"error": "requestor did not provide a valid, base 64 encoded authz grant"})
		context.Abort()
		return
	}

	txClient, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.Api.Rpc, conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
	if err != nil {
		config.Logger.Error("GetOsmosisTxClient", zap.Error(err))
		context.JSON(http.StatusBadRequest, "server misconfiguration (query client error), please notify administrator")
		return
	}

	//RPC request to check the TX. Will check signature as well.
	checkTxResp, err := txClient.BroadcastTxSync(txBytes)
	if err != nil || checkTxResp == nil {
		config.Logger.Error("BroadcastTxSync", zap.Error(err))
		context.JSON(http.StatusBadRequest, "failed to verify user address (1)")
		return
	}

	userTx := checkTxResp.GetTx()
	if userTx == nil || len(userTx.GetMsgs()) != 1 {
		context.JSON(http.StatusBadRequest, "failed to verify user address (2)")
		return
	}

	msg := userTx.GetMsgs()[0]
	authzGrant, ok := msg.(*authz.MsgGrant)
	if !ok {
		config.Logger.Error("TX is not an authz grant", zap.String("cosmos TX", "could not convert TX to authz grant type"))
		context.JSON(http.StatusBadRequest, "failed to verify user address (3)")
		return
	}

	if authzGrant.Grantee != HotWalletAddress {
		config.Logger.Error("TX grantee", zap.String("cosmos TX", "TX grantee '"+authzGrant.Grantee+"' does not match expected grantee for hot wallet"))
		context.JSON(http.StatusBadRequest, "failed to verify user address (4)")
		return
	} else if !strings.HasPrefix("osmo", authzGrant.Granter) {
		config.Logger.Error("TX is not an authz grant", zap.String("cosmos TX", "Invalid granter"))
		context.JSON(http.StatusBadRequest, "failed to verify user address (5)")
		return
	} else if authzGrant.Grant.GetAuthorization() == nil {
		config.Logger.Error("TX is not an authz grant", zap.String("cosmos TX", "Invalid grant authorization"))
		context.JSON(http.StatusBadRequest, "failed to verify user address (6)")
		return
	}

	grantAuthorization := authzGrant.Grant.GetAuthorization()
	grantType := grantAuthorization.MsgTypeURL()

	//TODO: need to test and make sure the type starts with a slash, but I think so based on the CLI command I tested.
	if grantType != "/osmosis.gamm.v1beta1.MsgSwapExactAmountIn" {
		config.Logger.Error("TX is not an authz grant", zap.String("cosmos TX", "Invalid grant authorization"))
		context.JSON(http.StatusBadRequest, "failed to verify user address (7)")
		return
	} else if time.Now().After(authzGrant.Grant.Expiration) {
		config.Logger.Error("TX authz grant is expired")
		context.JSON(http.StatusBadRequest, "failed to verify user address (8)")
		return
	} else if authzGrant.Grant.Expiration.Sub(time.Now()).Hours() >= 24 {
		config.Logger.Error("TX authz grant", zap.Float64("grant authorization too long", authzGrant.Grant.Expiration.Sub(time.Now()).Hours()))
		context.JSON(http.StatusBadRequest, "failed to verify user address (9)")
		return
	}

	tokenString, err := GenerateJWT(request.Address)
	if err != nil {
		context.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		context.Abort()
		return
	}
	context.JSON(http.StatusOK, gin.H{"token": tokenString})
}

func GenerateJWT(address string) (tokenString string, err error) {
	expirationTime := time.Now().Add(24 * time.Hour)

	claims := &JWTClaim{
		Address: address,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err = token.SignedString(jwtKey)
	return
}
