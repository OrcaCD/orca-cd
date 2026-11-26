package routes

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/apitoken"
	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/seclog"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// oauthRefreshReuseGrace is how long a replaced refresh token is only
	// refused, before using it revokes the grant.
	oauthRefreshReuseGrace = time.Minute
	// maxUsedRefreshTokens is how many replaced refresh tokens a grant keeps,
	// four days of hourly refreshes.
	maxUsedRefreshTokens = 100
)

var errTooManyAPITokens = fmt.Errorf("the user can't have more than %d api tokens, they have to revoke one first", maxAPITokensPerUser)

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// OAuthTokenHandler exchanges a code or a refresh token for an access token.
// It is called by the client, not the browser, so it ignores the session.
func OAuthTokenHandler(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")

	if err := c.Request.ParseForm(); err != nil {
		oauthError(c, http.StatusBadRequest, "invalid_request", "the body must be form encoded")
		return
	}
	for key, values := range c.Request.PostForm {
		if len(values) > 1 {
			oauthError(c, http.StatusBadRequest, "invalid_request", "the parameter "+key+" is repeated")
			return
		}
	}

	// Clients are public and prove themselves with PKCE. Credentials are
	// refused before a code or refresh token is used, as clients like
	// golang.org/x/oauth2 try them first and retry without (RFC 6749 section
	// 5.2, draft-ietf-oauth-client-id-metadata-document section 5).
	form := c.Request.PostForm
	if authorization := c.GetHeader("Authorization"); authorization != "" || form.Get("client_secret") != "" || form.Has("client_assertion") {
		if scheme, _, _ := strings.Cut(authorization, " "); strings.EqualFold(scheme, "Basic") {
			c.Header("WWW-Authenticate", `Basic realm="OrcaCD"`)
		}
		oauthError(c, http.StatusUnauthorized, "invalid_client", "clients must not authenticate, they are public clients that use PKCE")
		return
	}
	grantType := form.Get("grant_type")
	if grantType != grantTypeAuthorizationCode && grantType != grantTypeRefreshToken {
		oauthError(c, http.StatusBadRequest, "unsupported_grant_type", "only the authorization_code and refresh_token grant types are supported")
		return
	}
	if form.Get("client_id") == "" {
		oauthError(c, http.StatusBadRequest, "invalid_request", "the client_id is required")
		return
	}

	if grantType == grantTypeAuthorizationCode {
		exchangeOAuthCode(c)
	} else {
		refreshOAuthToken(c)
	}
}

func exchangeOAuthCode(c *gin.Context) {
	form := c.Request.PostForm
	code := form.Get("code")
	clientId := form.Get("client_id")
	invalidGrant := func() {
		oauthError(c, http.StatusBadRequest, "invalid_grant", "the code is invalid, expired or was already used")
	}

	entry, ok, reusedTokenId := oauthCodes.redeem(code)
	if reusedTokenId != "" {
		// The code leaked, so the token it got may be in the wrong hands too.
		token, err := gorm.G[models.APIToken](db.DB).Where("id = ?", reusedTokenId).First(c.Request.Context())
		switch {
		case err == nil:
			revokeOAuthToken(c, &token, "oauth_code_reused")
		case !errors.Is(err, gorm.ErrRecordNotFound):
			utils.Log.Error().Err(err).Str("api_token_id", reusedTokenId).Msg("Failed to revoke the OAuth token of a reused code")
		}
		invalidGrant()
		return
	}
	if !ok {
		invalidGrant()
		return
	}

	redirectURI := form.Get("redirect_uri")
	if entry.clientId != clientId ||
		(entry.redirectURIGiven && redirectURI != entry.redirectURI) ||
		(redirectURI != "" && redirectURI != entry.redirectURI) ||
		!verifyCodeChallenge(form.Get("code_verifier"), entry.codeChallenge) {
		invalidGrant()
		return
	}
	if resource := form.Get("resource"); resource != "" && (!isOAuthResource(resource) || (entry.resource != "" && resource != entry.resource)) {
		oauthError(c, http.StatusBadRequest, "invalid_target", "the resource is not the one that was authorized")
		return
	}

	ctx := c.Request.Context()
	user, err := gorm.G[models.User](db.DB).Where("id = ?", entry.userId).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			invalidGrant()
			return
		}
		oauthError(c, http.StatusInternalServerError, "server_error", "internal server error")
		return
	}

	// The audit log names the user that approved the client.
	auth.SetUser(c, &user)
	token, access, refresh, replaced, err := createOAuthToken(c, &user, clientId, entry.clientName, entry.scopes)
	if err != nil {
		if errors.Is(err, errTooManyAPITokens) {
			oauthError(c, http.StatusBadRequest, "invalid_grant", err.Error())
			return
		}
		utils.Log.Error().Err(err).Msg("Failed to create OAuth token")
		oauthError(c, http.StatusInternalServerError, "server_error", "internal server error")
		return
	}
	for range replaced {
		utils.RecordAuditLog(c, "revoked-api-token", "user", user.Id)
	}
	if oauthCodes.setToken(code, token.Id) {
		// The code was used again while the token was created.
		revokeOAuthToken(c, token, "oauth_code_reused")
		invalidGrant()
		return
	}
	utils.RecordAuditLog(c, "created", "api-token", token.Id)
	sse.PublishUpdate(sse.TopicAPITokensOf(user.Id), sse.TopicAdminAPITokens)

	respondOAuthToken(c, token, access, refresh)
}

// createOAuthToken creates the token of a grant. The grant replaces an earlier
// one of the same client, which happens when the user authorizes it again, and
// the ended grants of the user. It returns the IDs of the replaced tokens.
func createOAuthToken(c *gin.Context, user *models.User, clientId, clientName string, scopes []models.APITokenScope) (*models.APIToken, string, string, []string, error) {
	now := oauthNow().UTC()
	access := apitoken.Generate()
	tokenId, err := uuid.NewV7()
	if err != nil {
		return nil, "", "", nil, err
	}
	refresh, refreshHash := newRefreshToken(tokenId.String())
	refreshExpiresAt := now.Add(oauthRefreshTokenLifetime)
	token := models.APIToken{
		Id:               tokenId.String(),
		UserId:           user.Id,
		TokenHash:        access.Hash,
		Prefix:           access.Prefix,
		Scopes:           scopes,
		ExpiresAt:        now.Add(oauthAccessTokenLifetime),
		OAuthClientId:    &clientId,
		RefreshTokenHash: &refreshHash,
		RefreshExpiresAt: &refreshExpiresAt,
	}

	var replaced []string
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		ctx := c.Request.Context()
		tokens, err := gorm.G[models.APIToken](tx).Where("user_id = ?", user.Id).Find(ctx)
		if err != nil {
			return err
		}
		var kept []models.APIToken
		kept, replaced = replacedAPITokens(tokens, clientId)
		if len(replaced) > 0 {
			if _, err := gorm.G[models.APIToken](tx).Where("id IN ?", replaced).Delete(ctx); err != nil {
				return err
			}
		}
		if len(kept) >= maxAPITokensPerUser {
			return errTooManyAPITokens
		}
		names := make([]string, len(kept))
		for i := range kept {
			names[i] = strings.ToLower(kept[i].Name)
		}
		token.Name = uniqueTokenName(clientName, names)
		return gorm.G[models.APIToken](tx).Create(ctx, &token)
	})
	if err != nil {
		return nil, "", "", nil, err
	}
	return &token, access.Plain, refresh, replaced, nil
}

// replacedAPITokens splits the tokens of a user into the ones a new grant of
// the client keeps and the IDs of the ones it replaces: the earlier grant of
// the client and the ended grants of all clients, which would otherwise count
// towards the limit. A client with a metadata document keeps its earlier
// grants, as all its installs share the client ID, so replacing them would
// log out the app on the user's other machines.
func replacedAPITokens(tokens []models.APIToken, clientId string) ([]models.APIToken, []string) {
	now := oauthNow()
	sameInstall := func(token *models.APIToken) bool {
		return *token.OAuthClientId == clientId && !isOAuthClientMetadataURL(clientId)
	}
	var kept []models.APIToken
	var replaced []string
	for _, token := range tokens {
		if token.IsOAuth() && (sameInstall(&token) || !now.Before(token.GrantExpiresAt())) {
			replaced = append(replaced, token.Id)
			continue
		}
		kept = append(kept, token)
	}
	return kept, replaced
}

// uniqueTokenName numbers the name when the user has a token with it, as names
// are unique per user, ignoring case. takenNames are lower case.
func uniqueTokenName(name string, takenNames []string) string {
	candidate := name
	for i := 2; slices.Contains(takenNames, strings.ToLower(candidate)); i++ {
		candidate = fmt.Sprintf("%s (%d)", name, i)
	}
	return candidate
}

// newRefreshToken returns a refresh token and its hash. It starts with the id
// of the grant, so a replaced one that comes back can be told from a wrong one.
func newRefreshToken(tokenId string) (string, string) {
	plain := tokenId + "." + rand.Text() + rand.Text()
	return plain, apitoken.Hash(plain)
}

// usedRefreshTokenHash is what is kept of a replaced refresh token: enough to
// recognize it, as nothing can be done with it anymore.
func usedRefreshTokenHash(hash string) string {
	return hash[:32]
}

func refreshOAuthToken(c *gin.Context) {
	form := c.Request.PostForm
	plain := form.Get("refresh_token")
	invalidGrant := func() {
		oauthError(c, http.StatusBadRequest, "invalid_grant", "the refresh token is invalid, expired or revoked")
	}
	tokenId, _, ok := strings.Cut(plain, ".")
	if !ok || uuid.Validate(tokenId) != nil {
		invalidGrant()
		return
	}

	ctx := c.Request.Context()
	token, err := gorm.G[models.APIToken](db.DB).Where("id = ?", tokenId).First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		invalidGrant()
		return
	}
	if err != nil {
		oauthError(c, http.StatusInternalServerError, "server_error", "internal server error")
		return
	}
	if !token.IsOAuth() || token.RefreshTokenHash == nil {
		invalidGrant()
		return
	}

	now := oauthNow().UTC()
	hash := apitoken.Hash(plain)
	if subtle.ConstantTimeCompare([]byte(hash), []byte(*token.RefreshTokenHash)) != 1 {
		// A replaced refresh token is only used again when it was copied, so
		// whoever has the current one may not be the client (RFC 9700 section
		// 4.14.2). Right after the refresh, it is a client that refreshed twice
		// at once instead, which only fails.
		used := token.UsedRefreshTokenHashes
		if i := slices.Index(used, usedRefreshTokenHash(hash)); i >= 0 {
			justReplaced := i == len(used)-1 && token.RefreshedAt != nil && now.Sub(*token.RefreshedAt) < oauthRefreshReuseGrace
			if !justReplaced {
				revokeOAuthToken(c, &token, "oauth_refresh_token_reused")
			}
		}
		invalidGrant()
		return
	}

	if *token.OAuthClientId != form.Get("client_id") || token.RefreshExpiresAt == nil || !now.Before(*token.RefreshExpiresAt) {
		invalidGrant()
		return
	}

	// A client may not ask for more scopes. It gets the granted ones when it
	// asks for fewer, as the access token is the grant, and the response says
	// so (RFC 6749 section 5.1).
	if raw := form.Get("scope"); raw != "" {
		requested, err := parseOAuthScopes(raw)
		if err != nil || slices.ContainsFunc(requested, func(scope models.APITokenScope) bool { return !token.HasScope(scope) }) {
			oauthError(c, http.StatusBadRequest, "invalid_scope", "the scope can't be more than was granted")
			return
		}
	}

	// A grant lasts at most as long as an API token can.
	refreshExpiresAt := now.Add(oauthRefreshTokenLifetime)
	if limit := maxAPITokenExpiry(token.CreatedAt); refreshExpiresAt.After(limit) {
		refreshExpiresAt = limit
	}
	expiresAt := now.Add(oauthAccessTokenLifetime)
	if expiresAt.After(refreshExpiresAt) {
		expiresAt = refreshExpiresAt
	}
	used := append(slices.Clone(token.UsedRefreshTokenHashes), usedRefreshTokenHash(hash))
	if len(used) > maxUsedRefreshTokens {
		used = used[len(used)-maxUsedRefreshTokens:]
	}
	access := apitoken.Generate()
	refresh, refreshHash := newRefreshToken(token.Id)
	// Matching the old refresh token makes one of two refreshes at once fail.
	updated, err := gorm.G[models.APIToken](db.DB).
		Where("id = ? AND refresh_token_hash = ?", token.Id, hash).
		Updates(ctx, models.APIToken{
			TokenHash:              access.Hash,
			Prefix:                 access.Prefix,
			ExpiresAt:              expiresAt,
			RefreshTokenHash:       &refreshHash,
			UsedRefreshTokenHashes: used,
			RefreshExpiresAt:       &refreshExpiresAt,
			RefreshedAt:            &now,
		})
	if err != nil {
		oauthError(c, http.StatusInternalServerError, "server_error", "internal server error")
		return
	}
	if updated == 0 {
		invalidGrant()
		return
	}
	token.ExpiresAt = expiresAt
	sse.PublishUpdate(sse.TopicAPITokensOf(token.UserId), sse.TopicAdminAPITokens)

	respondOAuthToken(c, &token, access.Plain, refresh)
}

// revokeOAuthToken revokes the token of a grant whose secrets leaked.
func revokeOAuthToken(c *gin.Context, token *models.APIToken, reason string) {
	if _, err := gorm.G[models.APIToken](db.DB).Where("id = ?", token.Id).Delete(c.Request.Context()); err != nil {
		utils.Log.Error().Err(err).Str("api_token_id", token.Id).Msg("Failed to revoke OAuth token")
		return
	}
	seclog.Warn(c, seclog.TokenRejected, "reason", reason, "user_id", token.UserId, "api_token_id", token.Id)
	utils.RecordAuditLog(c, "revoked-api-token", "user", token.UserId)
	sse.PublishUpdate(sse.TopicAPITokensOf(token.UserId), sse.TopicAdminAPITokens)
}

func respondOAuthToken(c *gin.Context, token *models.APIToken, access, refresh string) {
	c.JSON(http.StatusOK, oauthTokenResponse{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int64(token.ExpiresAt.Sub(oauthNow()).Seconds()),
		RefreshToken: refresh,
		Scope:        joinScopes(token.Scopes),
	})
}

// isValidCodeChallenge checks that the challenge is a base64url SHA-256 hash.
func isValidCodeChallenge(challenge string) bool {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(challenge)
	return err == nil && len(decoded) == sha256.Size
}

// verifyCodeChallenge checks the PKCE verifier against the challenge (RFC 7636).
func verifyCodeChallenge(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 || strings.ContainsFunc(verifier, isNotUnreserved) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

// isNotUnreserved reports whether r is not one of the characters a verifier is
// made of.
func isNotUnreserved(r rune) bool {
	switch {
	case 'A' <= r && r <= 'Z', 'a' <= r && r <= 'z', '0' <= r && r <= '9', strings.ContainsRune("-._~", r):
		return false
	}
	return true
}
