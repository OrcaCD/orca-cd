package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	stateCookieName = "orcacd_oidc_state"
	stateTTL        = 10 * time.Minute
)

var ErrEmailNotVerified = errors.New("email is not verified")

var log = logger.New("oidc", false)

type OIDCUser struct {
	Subject string
	Email   string
	Name    string
	Picture string
	Issuer  string
}

func normalizePictureURL(raw string) string {
	picture := strings.TrimSpace(raw)
	if picture == "" {
		return ""
	}

	parsed, err := url.Parse(picture)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "http" {
		return ""
	}

	return picture
}

type stateData struct {
	State      string `json:"s"`
	Verifier   string `json:"v"`
	Nonce      string `json:"n"`
	ProviderId string `json:"p"`
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
		ClientID:     provider.ClientId,
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

// oidcHTTPClient is the HTTP client used for OIDC provider discovery and token
// exchange. Replaced with an unrestricted client in tests.
var oidcHTTPClient = httpclient.Default

// oidcContext injects the shared HTTP client into ctx so that go-oidc and
// oauth2 use it for all outbound requests (discovery, token exchange, etc.).
func oidcContext(ctx context.Context) context.Context {
	return gooidc.ClientContext(ctx, oidcHTTPClient)
}

func StartAuth(ctx context.Context, provider *models.OIDCProvider, appURL string) (authURL string, encryptedState string, err error) {
	ctx = oidcContext(ctx)
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
		ProviderId: provider.Id,
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

type profileClaims struct {
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	Nonce         string `json:"nonce"`
}

// mergeUserInfoClaims fills the claims missing from the id_token with the ones of the userinfo
// endpoint. Claims of the id_token take precedence and failures are ignored, so providers
// without a userinfo endpoint behave as before.
func mergeUserInfoClaims(ctx context.Context, provider *gooidc.Provider, token *oauth2.Token, subject string, claims *profileClaims) {
	userInfo, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil {
		log.Debug().Err(err).Msg("could not fetch userinfo")
		return
	}

	// The userinfo response must only be used if it belongs to the user of the id_token
	if userInfo.Subject != subject {
		log.Warn().Msg("userinfo subject does not match the id_token subject")
		return
	}

	var info profileClaims
	if err := userInfo.Claims(&info); err != nil {
		log.Debug().Err(err).Msg("could not parse userinfo claims")
		return
	}

	if claims.Email == "" {
		claims.Email = info.Email
		claims.EmailVerified = info.EmailVerified
	} else if claims.EmailVerified == nil && strings.EqualFold(claims.Email, info.Email) {
		claims.EmailVerified = info.EmailVerified
	}
	if claims.Name == "" {
		claims.Name = info.Name
	}
	if claims.Picture == "" {
		claims.Picture = info.Picture
	}
}

func HandleCallback(ctx context.Context, provider *models.OIDCProvider, appURL string, code string, stateParam string, encryptedState string) (*OIDCUser, error) {
	ctx = oidcContext(ctx)
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

	if sd.ProviderId != provider.Id {
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

	rawIdToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	verifier := oidcProvider.Verifier(&gooidc.Config{
		ClientID: provider.ClientId,
	})

	idToken, err := verifier.Verify(ctx, rawIdToken)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}

	var claims profileClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}

	if claims.Nonce != sd.Nonce {
		return nil, fmt.Errorf("nonce mismatch")
	}

	// Some providers only put the subject into the id_token and serve the profile at the userinfo endpoint
	if claims.Email == "" || claims.EmailVerified == nil {
		mergeUserInfoClaims(ctx, oidcProvider, oauth2Token, idToken.Subject, &claims)
	}

	if claims.Email == "" {
		return nil, fmt.Errorf("email claim is missing from id_token and userinfo")
	}

	if provider.RequireVerifiedEmail && (claims.EmailVerified == nil || !*claims.EmailVerified) {
		return nil, ErrEmailNotVerified
	}

	email := strings.ToLower(strings.TrimSpace(claims.Email))

	name := claims.Name
	if name == "" {
		name = email
	}

	picture := normalizePictureURL(claims.Picture)

	return &OIDCUser{
		Subject: idToken.Subject,
		Email:   email,
		Name:    name,
		Picture: picture,
		Issuer:  idToken.Issuer,
	}, nil
}

func StateCookieName() string {
	return stateCookieName
}
