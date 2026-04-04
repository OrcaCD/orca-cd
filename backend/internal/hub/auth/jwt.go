package auth

import (
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/golang-jwt/jwt/v5"
)

var (
	privateKey  ed25519.PrivateKey
	issuer      string
	userParser  *jwt.Parser
	agentParser *jwt.Parser
)

const tokenExpiry = 24 * time.Hour

type UserClaims struct {
	jwt.RegisteredClaims
	Name  string `json:"name"`
	Email string `json:"email"`
}

type AgentClaims struct {
	jwt.RegisteredClaims
	KeyId string `json:"kid"` // To invalidate old tokens when a new one is issued
}

func initJWT(appSecret, appURL string) error {
	seed, err := hkdf.Key(sha256.New, []byte(appSecret), nil, "JWT_SIGNING_KEY", ed25519.SeedSize)
	if err != nil {
		return fmt.Errorf("auth.initJWT: %w", err)
	}

	privateKey = ed25519.NewKeyFromSeed(seed)
	issuer = appURL

	userParser = jwt.NewParser(
		jwt.WithIssuer(appURL),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuedAt(),
		jwt.WithNotBeforeRequired(),
		jwt.WithStrictDecoding(),
		jwt.WithAudience("user"),
	)

	agentParser = jwt.NewParser(
		jwt.WithIssuer(appURL),
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuedAt(),
		jwt.WithNotBeforeRequired(),
		jwt.WithStrictDecoding(),
		jwt.WithAudience("agent"),
	)
	return nil
}

func GenerateUserToken(user *models.User) (string, error) {
	now := time.Now()

	claims := UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   user.Id,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenExpiry)),
			Audience:  []string{"user"},
		},
		Name:  user.Name,
		Email: user.Email,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	return token.SignedString(privateKey)
}

func ValidateUserToken(tokenString string) (*UserClaims, error) {
	token, err := userParser.ParseWithClaims(tokenString, &UserClaims{}, func(*jwt.Token) (any, error) {
		return privateKey.Public(), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*UserClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	if claims.Subject == "" {
		return nil, fmt.Errorf("invalid token claims: missing subject")
	}

	return claims, nil
}

func GenerateAgentToken(agent *models.Agent) (string, error) {
	if agent.Id == "" {
		return "", fmt.Errorf("agent ID is required for token generation")
	}

	now := time.Now()

	claims := AgentClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   agent.Id,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Audience:  []string{"agent"},
		},
		KeyId: agent.KeyId.String(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	return token.SignedString(privateKey)
}

func ValidateAgentToken(tokenString string) (*AgentClaims, error) {
	token, err := agentParser.ParseWithClaims(tokenString, &AgentClaims{}, func(*jwt.Token) (any, error) {
		return privateKey.Public(), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*AgentClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	if claims.Subject == "" {
		return nil, fmt.Errorf("invalid token claims: missing subject")
	}

	return claims, nil
}
