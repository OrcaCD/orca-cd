package routes

import (
	"crypto/rand"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxOAuthStateLength = 1024
	// maxOAuthCodes bounds the codes waiting to be exchanged, which only users
	// that are logged in can create, and maxOAuthCodesPerUser keeps one user
	// from taking all of them.
	maxOAuthCodes        = 1000
	maxOAuthCodesPerUser = 10
)

// oauthAuthorizationRequest is a validated request of a client to be
// authorized.
type oauthAuthorizationRequest struct {
	clientId string
	client   *oauthClient
	// redirectURI is where the user returns to. redirectURIGiven tells whether
	// the client named it, so the token request has to name it again.
	redirectURI      string
	redirectURIGiven bool
	state            string
	codeChallenge    string
	resource         string
	scopes           []models.APITokenScope
}

// oauthRequestError is an invalid authorization request. Once the client and
// its redirect URI are known, the user is sent back to the client with the
// error, before that, the consent page shows it.
type oauthRequestError struct {
	code        string
	description string
	redirectURL string
}

func parseOAuthAuthorizationRequest(query url.Values) (*oauthAuthorizationRequest, *oauthRequestError) {
	// Parameters must not be repeated (RFC 6749 section 3.1).
	for key, values := range query {
		if len(values) > 1 {
			return nil, &oauthRequestError{code: "invalid_request", description: "the parameter " + key + " is repeated"}
		}
	}

	clientId := query.Get("client_id")
	client, ok := decodeOAuthClientId(clientId)
	if !ok {
		return nil, &oauthRequestError{code: "invalid_client", description: "the client is not registered, it has to register again"}
	}
	redirectURI, ok := client.redirectURI(query.Get("redirect_uri"))
	if !ok {
		return nil, &oauthRequestError{code: "invalid_request", description: "the redirect_uri is not registered for the client"}
	}

	req := &oauthAuthorizationRequest{
		clientId:         clientId,
		client:           client,
		redirectURI:      redirectURI,
		redirectURIGiven: query.Get("redirect_uri") != "",
		state:            query.Get("state"),
		codeChallenge:    query.Get("code_challenge"),
		resource:         query.Get("resource"),
	}
	fail := func(code, description string) (*oauthAuthorizationRequest, *oauthRequestError) {
		return nil, &oauthRequestError{
			code:        code,
			description: description,
			redirectURL: req.redirectURL(url.Values{"error": {code}, "error_description": {description}}),
		}
	}

	if len(req.state) > maxOAuthStateLength {
		req.state = ""
		return fail("invalid_request", "the state is too long")
	}
	if query.Get("response_type") != "code" {
		return fail("unsupported_response_type", "only the code response type is supported")
	}
	if query.Get("code_challenge_method") != "S256" || !isValidCodeChallenge(req.codeChallenge) {
		return fail("invalid_request", "a code_challenge with the S256 method is required")
	}
	if req.resource != "" && !isOAuthResource(req.resource) {
		return fail("invalid_target", "the resource is not this OrcaCD hub")
	}
	scopes, err := parseOAuthScopes(query.Get("scope"))
	if err != nil {
		return fail("invalid_scope", err.Error())
	}
	req.scopes = scopes
	return req, nil
}

// redirectURL adds the parameters, the state and the issuer to the redirect
// URI. The issuer lets the client check where the response comes from (RFC 9207).
func (r *oauthAuthorizationRequest) redirectURL(params url.Values) string {
	u, _ := url.Parse(r.redirectURI) // validated when the client registered
	query := u.Query()
	maps.Copy(query, params)
	if r.state != "" {
		query.Set("state", r.state)
	}
	query.Set("iss", oauthIssuer)
	u.RawQuery = query.Encode()
	return u.String()
}

func respondOAuthRequestError(c *gin.Context, err *oauthRequestError) {
	body := gin.H{"error": err.description}
	if err.redirectURL != "" {
		body["redirectUrl"] = err.redirectURL
	}
	c.JSON(http.StatusBadRequest, body)
}

type oauthScopeResponse struct {
	Scope models.APITokenScope `json:"scope"`
	// Grantable tells whether the user has the permission to give the scope.
	Grantable bool `json:"grantable"`
}

type oauthAuthorizationResponse struct {
	ClientName  string               `json:"clientName"`
	RedirectURI string               `json:"redirectUri"`
	Scopes      []oauthScopeResponse `json:"scopes"`
	// CanAuthorize tells whether the user may create API tokens.
	CanAuthorize bool `json:"canAuthorize"`
}

// OAuthAuthorizationRequestHandler checks the authorization request the
// consent page was opened with, and returns what the page shows.
func OAuthAuthorizationRequestHandler(c *gin.Context) {
	req, reqErr := parseOAuthAuthorizationRequest(c.Request.URL.Query())
	if reqErr != nil {
		respondOAuthRequestError(c, reqErr)
		return
	}
	perms, ok := currentPermissions(c)
	if !ok {
		return
	}

	scopes := make([]oauthScopeResponse, 0, len(req.scopes))
	for _, scope := range req.scopes {
		_, ungranted := ungrantedAPITokenScope(perms, []models.APITokenScope{scope})
		scopes = append(scopes, oauthScopeResponse{Scope: scope, Grantable: !ungranted})
	}
	c.JSON(http.StatusOK, oauthAuthorizationResponse{
		ClientName:   req.client.Name,
		RedirectURI:  req.redirectURI,
		Scopes:       scopes,
		CanAuthorize: perms.CanAny(models.ResourceAPIToken, models.ActionCreate),
	})
}

type oauthAuthorizeRequest struct {
	// Query is the query string the consent page was opened with. It is
	// checked again, so the page can't change what the client asked for.
	Query   string                 `json:"query"`
	Approve bool                   `json:"approve"`
	Scopes  []models.APITokenScope `json:"scopes"`
}

// OAuthAuthorizeHandler answers the consent page. It returns where to send
// the user: back to the client with a code, or with an error when they denied
// it.
func OAuthAuthorizeHandler(c *gin.Context) {
	user, ok := auth.GetUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return
	}
	var body oauthAuthorizeRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	query, err := url.ParseQuery(body.Query)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid authorization request"})
		return
	}
	req, reqErr := parseOAuthAuthorizationRequest(query)
	if reqErr != nil {
		respondOAuthRequestError(c, reqErr)
		return
	}

	if !body.Approve {
		c.JSON(http.StatusOK, gin.H{"redirectUrl": req.redirectURL(url.Values{
			"error":             {"access_denied"},
			"error_description": {"the user denied the request"},
		})})
		return
	}

	perms, ok := currentPermissions(c)
	if !ok {
		return
	}
	if !perms.CanAny(models.ResourceAPIToken, models.ActionCreate) {
		c.JSON(http.StatusForbidden, gin.H{"error": "you don't have the permission to create api tokens"})
		return
	}
	if len(body.Scopes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one scope is required"})
		return
	}
	for _, scope := range body.Scopes {
		if !slices.Contains(req.scopes, scope) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "the client didn't ask for the scope " + string(scope)})
			return
		}
	}
	scopes, err := normalizeAPITokenScopes(body.Scopes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if scope, ungranted := ungrantedAPITokenScope(perms, scopes); ungranted {
		c.JSON(http.StatusForbidden, gin.H{"error": "you don't have the permission for scope " + string(scope)})
		return
	}

	// Checked here too, so the user isn't sent back to a client that then
	// fails to get its token.
	tokens, err := gorm.G[models.APIToken](db.DB).Where("user_id = ?", user.Id).Find(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if kept, _ := replacedAPITokens(tokens, req.clientId); len(kept) >= maxAPITokensPerUser {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("you can't have more than %d api tokens, revoke one first", maxAPITokensPerUser)})
		return
	}

	code := rand.Text()
	if !oauthCodes.add(code, &oauthAuthorizationCode{
		clientId:         req.clientId,
		redirectURI:      req.redirectURI,
		redirectURIGiven: req.redirectURIGiven,
		codeChallenge:    req.codeChallenge,
		resource:         req.resource,
		userId:           user.Id,
		scopes:           scopes,
		expiresAt:        oauthNow().Add(oauthCodeLifetime),
	}) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "too many pending authorizations, try again in a minute"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"redirectUrl": req.redirectURL(url.Values{"code": {code}})})
}

// oauthAuthorizationCode is what the user approved, until the client exchanges
// the code for a token. Codes live a minute, so they are kept in memory.
type oauthAuthorizationCode struct {
	clientId         string
	redirectURI      string
	redirectURIGiven bool
	codeChallenge    string
	resource         string
	userId           string
	scopes           []models.APITokenScope
	expiresAt        time.Time

	redeemed bool
	// tokenId is the token the code was exchanged for, which is revoked when
	// the code is used again.
	tokenId string
	// reused tells that the code was used again while its token was created,
	// so the token is revoked once it is set.
	reused bool
}

// oauthCodeReuseWindow is how long an exchanged code is kept, to revoke its
// token when it is used again.
const oauthCodeReuseWindow = 10 * time.Minute

var oauthCodes = oauthCodeStore{codes: map[string]*oauthAuthorizationCode{}}

type oauthCodeStore struct {
	mu    sync.Mutex
	codes map[string]*oauthAuthorizationCode
}

func (code *oauthAuthorizationCode) isGone(now time.Time) bool {
	if code.redeemed {
		return now.After(code.expiresAt.Add(oauthCodeReuseWindow))
	}
	return now.After(code.expiresAt)
}

func (s *oauthCodeStore) add(code string, entry *oauthAuthorizationCode) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := oauthNow()
	pendingOfUser := 0
	for key, existing := range s.codes {
		if existing.isGone(now) {
			delete(s.codes, key)
		} else if existing.userId == entry.userId && !existing.redeemed {
			pendingOfUser++
		}
	}
	if len(s.codes) >= maxOAuthCodes || pendingOfUser >= maxOAuthCodesPerUser {
		return false
	}
	s.codes[code] = entry
	return true
}

// redeem returns the authorization of a code that wasn't used yet. A code can
// only be tried once. When it was already exchanged, someone else has the code,
// so it returns the token it was exchanged for to revoke it (RFC 6749 section
// 4.1.2). If that token isn't created yet, setToken reports it instead.
func (s *oauthCodeStore) redeem(code string) (entry oauthAuthorizationCode, ok bool, reusedTokenId string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := oauthNow()
	existing := s.codes[code]
	if existing == nil || existing.isGone(now) {
		return oauthAuthorizationCode{}, false, ""
	}
	if existing.redeemed {
		existing.reused = true
		return oauthAuthorizationCode{}, false, existing.tokenId
	}
	if now.After(existing.expiresAt) {
		return oauthAuthorizationCode{}, false, ""
	}
	existing.redeemed = true
	return *existing, true, ""
}

// setToken records the token a code was exchanged for. It reports whether the
// code was used again in the meantime, so the token has to be revoked.
func (s *oauthCodeStore) setToken(code, tokenId string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := s.codes[code]
	if existing == nil {
		return false
	}
	existing.tokenId = tokenId
	return existing.reused
}
