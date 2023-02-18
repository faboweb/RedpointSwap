package api

import (
	"github.com/golang-jwt/jwt/v4"
)

var Initialized bool

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
