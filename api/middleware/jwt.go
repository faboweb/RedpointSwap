package middleware

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

type TokenRequest struct {
	Address string `json:"address"`
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
