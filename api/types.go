package api

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/golang-jwt/jwt/v4"
)

var Initialized bool
var HotWalletAddress string
var HotWalletArbBalance sdk.Int

type JWTClaim struct {
	jwt.RegisteredClaims
}

var jwtKey []byte

func SetSecretKey(jwtSecret string) {
	jwtKey = []byte(jwtSecret)
}

func GetSecretKey() []byte {
	return jwtKey
}
