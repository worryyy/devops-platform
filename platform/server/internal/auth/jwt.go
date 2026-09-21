package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const TokenTTL = 12 * time.Hour

type Claims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// SignToken issues an HMAC-SHA256 token for an authenticated user.
func SignToken(secret, username, role string, now time.Time) (string, error) {
	if secret == "" {
		return "", errors.New("jwt secret is empty")
	}
	claims := Claims{
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   username,
			ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "platform-server",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// ParseToken validates signature and expiry, returning the claims.
func ParseToken(secret, token string) (Claims, error) {
	var claims Claims
	parsed, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return Claims{}, err
	}
	if !parsed.Valid {
		return Claims{}, errors.New("invalid token")
	}
	return claims, nil
}
