package endpoints

import (
	b64 "encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DefiantLabs/RedpointSwap/api"
	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/DefiantLabs/RedpointSwap/osmosis"
	"github.com/cosmos/cosmos-sdk/types"
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

func AuthzGranteeInfo(context *gin.Context) {
	context.JSON(http.StatusOK, &AuthzGranteeResponse{GranteeAddress: api.HotWalletAddress})
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

	txClientSubmit, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.GetApiRpcSubmitTxEndpoint(), conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
	if err != nil {
		config.Logger.Error("GetOsmosisTxClient", zap.Error(err))
		context.JSON(http.StatusBadRequest, "server misconfiguration (query client error), please notify administrator")
		return
	}

	txClientSearch, err := osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.GetApiRpcSearchTxEndpoint(), conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
	if err != nil {
		config.Logger.Error("GetOsmosisTxClient", zap.Error(err))
		context.JSON(http.StatusBadRequest, "server misconfiguration (query client error), please notify administrator")
		return
	}

	//RPC request to check the TX. Will check signature as well.
	checkTxResp, err := txClientSubmit.BroadcastTxSync(txBytes)
	if err != nil || checkTxResp == nil {
		config.Logger.Error("BroadcastTxSync", zap.Error(err))
		context.JSON(http.StatusBadRequest, "failed to verify user address (1)")
		return
	} else if checkTxResp.Code != 0 {
		config.Logger.Error("BroadcastTxSync", zap.Uint32("TX code", checkTxResp.Code))
		context.JSON(http.StatusBadRequest, "TX error code: "+fmt.Sprint(checkTxResp.Code))
		return
	}

	//Wait at most two blocks for the TX
	resp, err := osmosis.AwaitTx(txClientSearch, checkTxResp.TxHash, time.Second*13)
	if err != nil {
		//Try a different RPC endpoint
		txClientSearch, err = osmosis.GetOsmosisTxClient(conf.Api.ChainID, conf.GetApiRpcSearchTxEndpoint(), conf.Api.KeyringHomeDir, conf.Api.KeyringBackend, conf.Api.HotWalletKey)
		if err != nil {
			config.Logger.Error("GetOsmosisTxClient", zap.Error(err))
			context.JSON(http.StatusBadRequest, "server misconfiguration (query client error), please notify administrator")
			return
		}

		//Don't wait long since we already waited two blocks for the TX (this is just to search on a diff node)
		resp, err = osmosis.AwaitTx(txClientSearch, checkTxResp.TxHash, time.Second*2)
		if err != nil {
			config.Logger.Error("AwaitTx", zap.Error(err))
			context.JSON(http.StatusBadRequest, "failed to verify user address (7)")
			return
		}
	}

	var userTx types.Tx
	codec := osmosis.GetCodec()
	codec.InterfaceRegistry.UnpackAny(resp.GetTxResponse().Tx, &userTx)

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

	if authzGrant.Grantee != api.HotWalletAddress {
		config.Logger.Error("TX grantee", zap.String("cosmos TX", "TX grantee '"+authzGrant.Grantee+"' does not match expected grantee for hot wallet"))
		context.JSON(http.StatusBadRequest, "failed to verify user address (4)")
		return
	} else if !strings.HasPrefix(authzGrant.Granter, "osmo") {
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
	secondsUntilGrantExpires := time.Until(authzGrant.Grant.Expiration).Seconds()

	//TODO: need to test and make sure the type starts with a slash, but I think so based on the CLI command I tested.
	if grantType != "/osmosis.gamm.v1beta1.MsgSwapExactAmountIn" {
		config.Logger.Error("TX is not an authz grant", zap.String("cosmos TX", "Invalid grant authorization"))
		context.JSON(http.StatusBadRequest, "authz grant is not valid")
		return
	} else if time.Now().After(authzGrant.Grant.Expiration) {
		config.Logger.Error("TX authz grant is expired")
		context.JSON(http.StatusBadRequest, "grant is already expired")
		return
	} else if secondsUntilGrantExpires >= conf.Authz.MaximumAuthzGrantSeconds {
		config.Logger.Error("TX authz grant", zap.Float64("grant authorization too far in the future", secondsUntilGrantExpires))
		context.JSON(http.StatusBadRequest, fmt.Sprintf("grant expiration must be no more than %f seconds", conf.Authz.MaximumAuthzGrantSeconds))
		return
	}

	tokenString, err := GenerateJWT(authzGrant.Grant.Expiration, request.Address)
	if err != nil {
		context.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		context.Abort()
		return
	}
	context.JSON(http.StatusOK, gin.H{"token": tokenString})
}

func GenerateJWT(expirationTime time.Time, address string) (tokenString string, err error) {
	claims := &api.JWTClaim{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   address,
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    config.Conf.JWT.Issuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err = token.SignedString(api.GetSecretKey())
	return
}
