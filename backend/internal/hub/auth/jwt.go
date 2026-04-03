package auth

import (
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	privateKey ed25519.PrivateKey
	issuer     string
	parser     *jwt.Parser
)

const tokenExpiry = 24 * time.Hour

type Claims struct {
	jwt.RegisteredClaims
	UserId   string `json:"uid"`
	Username string `json:"usr"`
}

func initJWT(appSecret, appURL string) error {
	seed, err := hkdf.Key(sha256.New, []byte(appSecret), nil, "JWT_SIGNING_KEY", ed25519.SeedSize)
	if err != nil {
		return fmt.Errorf("auth.initJWT: %w", err)
	}

	privateKey = ed25519.NewKeyFromSeed(seed)
	issuer = appURL

	parser = jwt.NewParser(
		jwt.WithIssuer(appURL),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuedAt(),
		jwt.WithNotBeforeRequired(),
		jwt.WithStrictDecoding(),
	)
	return nil
}

func GenerateToken(userId string, username string) (string, error) {
	now := time.Now()

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   userId,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenExpiry)),
		},
		UserId:   userId,
		Username: username,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	return token.SignedString(privateKey)
}

func ValidateToken(tokenString string) (*Claims, error) {
	token, err := parser.ParseWithClaims(tokenString, &Claims{}, func(*jwt.Token) (any, error) {
		return privateKey.Public(), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}
