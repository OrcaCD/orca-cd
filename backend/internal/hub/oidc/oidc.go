package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	stateCookieName = "orcacd_oidc_state"
	stateTTL        = 10 * time.Minute
)

type OIDCUser struct {
	Subject string
	Email   string
	Name    string
	Issuer  string
}

type stateData struct {
	State      string `json:"s"`
	Verifier   string `json:"v"`
	Nonce      string `json:"n"`
	ProviderID string `json:"p"`
	ExpiresAt  int64  `json:"e"`
}

func buildOAuth2Config(provider *models.OIDCProvider, endpoint oauth2.Endpoint, appURL string) *oauth2.Config {
	scopes := []string{gooidc.ScopeOpenID, "email", "profile"}
	if provider.Scopes != "" {
		for s := range strings.SplitSeq(provider.Scopes, ",") {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				scopes = append(scopes, trimmed)
			}
		}
	}

	return &oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret.String(),
		Endpoint:     endpoint,
		RedirectURL:  fmt.Sprintf("%s/api/v1/auth/oidc/%s/callback", appURL, provider.Id),
		Scopes:       scopes,
	}
}

func generateRandomString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func encryptState(sd *stateData) (string, error) {
	data, err := json.Marshal(sd)
	if err != nil {
		return "", fmt.Errorf("marshal state: %w", err)
	}
	return crypto.Encrypt(string(data))
}

func decryptState(encrypted string) (*stateData, error) {
	decrypted, err := crypto.Decrypt(encrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt state: %w", err)
	}
	var sd stateData
	if err := json.Unmarshal([]byte(decrypted), &sd); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &sd, nil
}

func StartAuth(ctx context.Context, provider *models.OIDCProvider, appURL string) (authURL string, encryptedState string, err error) {
	oidcProvider, err := gooidc.NewProvider(ctx, provider.IssuerURL)
	if err != nil {
		return "", "", fmt.Errorf("discover provider: %w", err)
	}

	oauth2Config := buildOAuth2Config(provider, oidcProvider.Endpoint(), appURL)

	state, err := generateRandomString()
	if err != nil {
		return "", "", fmt.Errorf("generate state: %w", err)
	}

	verifier := oauth2.GenerateVerifier()

	nonce, err := generateRandomString()
	if err != nil {
		return "", "", fmt.Errorf("generate nonce: %w", err)
	}

	sd := &stateData{
		State:      state,
		Verifier:   verifier,
		Nonce:      nonce,
		ProviderID: provider.Id,
		ExpiresAt:  time.Now().Add(stateTTL).Unix(),
	}

	encryptedState, err = encryptState(sd)
	if err != nil {
		return "", "", fmt.Errorf("encrypt state: %w", err)
	}

	authURL = oauth2Config.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		gooidc.Nonce(nonce),
	)

	return authURL, encryptedState, nil
}

func HandleCallback(ctx context.Context, provider *models.OIDCProvider, appURL string, code string, stateParam string, encryptedState string) (*OIDCUser, error) {
	sd, err := decryptState(encryptedState)
	if err != nil {
		return nil, fmt.Errorf("invalid state cookie: %w", err)
	}

	if time.Now().Unix() > sd.ExpiresAt {
		return nil, fmt.Errorf("state expired")
	}

	if sd.State != stateParam {
		return nil, fmt.Errorf("state mismatch")
	}

	if sd.ProviderID != provider.Id {
		return nil, fmt.Errorf("provider mismatch")
	}

	oidcProvider, err := gooidc.NewProvider(ctx, provider.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover provider: %w", err)
	}

	oauth2Config := buildOAuth2Config(provider, oidcProvider.Endpoint(), appURL)

	oauth2Token, err := oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(sd.Verifier))
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	verifier := oidcProvider.Verifier(&gooidc.Config{
		ClientID: provider.ClientID,
	})

	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Nonce         string `json:"nonce"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}

	if claims.Nonce != sd.Nonce {
		return nil, fmt.Errorf("nonce mismatch")
	}

	if claims.Email == "" {
		return nil, fmt.Errorf("email claim is missing from id_token")
	}

	email := strings.ToLower(strings.TrimSpace(claims.Email))

	name := claims.Name
	if name == "" {
		name = email
	}

	return &OIDCUser{
		Subject: idToken.Subject,
		Email:   email,
		Name:    name,
		Issuer:  idToken.Issuer,
	}, nil
}

func StateCookieName() string {
	return stateCookieName
}
