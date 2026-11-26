package routes

import (
	"context"
	"crypto/rand"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

const (
	maxOAuthStateLength = 1024
	// maxOAuthCodes bounds the codes in memory, which only users that are
	// logged in can create, and maxOAuthCodesPerUser keeps one user from taking
	// all of them.
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

// oauthRequestErrorResponse is the 400 of the consent page. With a redirect
// URL, the page sends the user back to the client with the error.
type oauthRequestErrorResponse struct {
	Message     string `json:"error" required:"true" doc:"What is wrong with the request"`
	RedirectURL string `json:"redirectUrl,omitempty" doc:"Where to send the user, set once the client and its redirect URI are known"`
}

func (e *oauthRequestErrorResponse) Error() string {
	return e.Message
}

func (e *oauthRequestErrorResponse) GetStatus() int {
	return http.StatusBadRequest
}

func (e *oauthRequestError) response() error {
	return &oauthRequestErrorResponse{Message: e.description, RedirectURL: e.redirectURL}
}

// oauthRequestErrorResponses documents the 400 of the consent operations with
// its redirect URL, which Huma would document as a plain error. Their Errors
// leave out the 400, as Huma would replace this one.
func oauthRequestErrorResponses() map[string]*huma.Response {
	schema := restapi.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[oauthRequestErrorResponse](), true, "")
	return map[string]*huma.Response{
		strconv.Itoa(http.StatusBadRequest): {
			Description: "The authorization request or the answer is invalid",
			Content:     map[string]*huma.MediaType{"application/json": {Schema: schema}},
		},
	}
}

type oauthScopeResponse struct {
	Scope     models.APITokenScope `json:"scope" required:"true"`
	Grantable bool                 `json:"grantable" required:"true" doc:"Whether the user has the permission to give the scope"`
}

type oauthAuthorizationResponse struct {
	ClientName  string               `json:"clientName" required:"true" doc:"The name the client registered with, which it chose itself"`
	RedirectURI string               `json:"redirectUri" required:"true" doc:"Where the user returns to after answering"`
	Scopes      []oauthScopeResponse `json:"scopes" required:"true" doc:"The scopes the client asks for"`
	// CanAuthorize tells whether the user may create API tokens.
	CanAuthorize bool `json:"canAuthorize" required:"true" doc:"Whether the user has the permission to create API tokens"`
}

// oauthAuthorizationQuery documents the authorization request (RFC 6749
// section 4.1.1). The handler parses the raw query instead, to reject
// repeated parameters and to send errors back to the client once its redirect
// URI is known, so nothing is validated here.
type oauthAuthorizationQuery struct {
	ClientID            string `query:"client_id" doc:"The client ID from the registration"`
	RedirectURI         string `query:"redirect_uri" doc:"One of the registered redirect URIs, required when the client registered more than one"`
	ResponseType        string `query:"response_type" doc:"Must be code"`
	Scope               string `query:"scope" doc:"Space separated scopes, the MCP scopes when empty"`
	State               string `query:"state" doc:"Returned to the client unchanged"`
	CodeChallenge       string `query:"code_challenge" doc:"The PKCE code challenge"`
	CodeChallengeMethod string `query:"code_challenge_method" doc:"Must be S256"`
	Resource            string `query:"resource" doc:"The MCP endpoint or the hub"`
}

// OAuthAuthorizationRequestHandler checks the authorization request the
// consent page was opened with, and returns what the page shows.
var OAuthAuthorizationRequestHandler = restapi.Handler(huma.Operation{
	OperationID: "get-oauth-authorization-request",
	Method:      http.MethodGet,
	Path:        "/oauth/authorize",
	Summary:     "Check an authorization request",
	Description: "Checks the authorization request of an MCP client and returns what the consent page shows. " +
		"Errors with a redirect URL send the user back to the client.",
	Tags:      []string{tagAPITokens},
	Security:  restapi.SessionOnly,
	Errors:    []int{http.StatusInternalServerError},
	Responses: oauthRequestErrorResponses(),
}, func(ctx context.Context, _ *oauthAuthorizationQuery) (*struct{ Body oauthAuthorizationResponse }, error) {
	c := restapi.GinContext(ctx)
	req, reqErr := parseOAuthAuthorizationRequest(c.Request.URL.Query())
	if reqErr != nil {
		return nil, reqErr.response()
	}
	perms, err := permissionsFor(c)
	if err != nil {
		return nil, err
	}

	scopes := make([]oauthScopeResponse, 0, len(req.scopes))
	for _, scope := range req.scopes {
		_, ungranted := ungrantedAPITokenScope(perms, []models.APITokenScope{scope})
		scopes = append(scopes, oauthScopeResponse{Scope: scope, Grantable: !ungranted})
	}
	return &struct{ Body oauthAuthorizationResponse }{Body: oauthAuthorizationResponse{
		ClientName:   req.client.Name,
		RedirectURI:  req.redirectURI,
		Scopes:       scopes,
		CanAuthorize: perms.CanAny(models.ResourceAPIToken, models.ActionCreate),
	}}, nil
})

type oauthAuthorizeRequest struct {
	Query   string                 `json:"query" required:"true" doc:"The query string the consent page was opened with. It is checked again, so the page can't change what the client asked for."`
	Approve bool                   `json:"approve" required:"true" doc:"Whether the user allows the client"`
	Scopes  []models.APITokenScope `json:"scopes" doc:"The scopes the user gives, a part of the requested ones. Required when approving."`
}

type oauthRedirectResponse struct {
	RedirectURL string `json:"redirectUrl" required:"true" doc:"Where to send the user: back to the client with a code, or with an error after a denial"`
}

// OAuthAuthorizeHandler answers the consent page. It returns where to send
// the user: back to the client with a code, or with an error when they denied
// it.
var OAuthAuthorizeHandler = restapi.Handler(huma.Operation{
	OperationID: "answer-oauth-authorization-request",
	Method:      http.MethodPost,
	Path:        "/oauth/authorize",
	Summary:     "Answer an authorization request",
	Description: fmt.Sprintf("Allows or denies the authorization request of an MCP client. Allowing it gives the client an API token "+
		"with the chosen scopes, which only works on the MCP endpoint. A user can have at most %d tokens. ", maxAPITokensPerUser) +
		requires(models.ResourceAPIToken, models.ActionCreate),
	Tags:      []string{tagAPITokens},
	Security:  restapi.SessionOnly,
	Errors:    []int{http.StatusForbidden, http.StatusConflict, http.StatusServiceUnavailable},
	Responses: oauthRequestErrorResponses(),
}, func(ctx context.Context, input *struct{ Body oauthAuthorizeRequest }) (*struct{ Body oauthRedirectResponse }, error) {
	c := restapi.GinContext(ctx)
	user, ok := auth.GetUser(c)
	if !ok {
		return nil, huma.Error401Unauthorized("missing authentication")
	}
	body := input.Body
	query, err := url.ParseQuery(body.Query)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid authorization request")
	}
	req, reqErr := parseOAuthAuthorizationRequest(query)
	if reqErr != nil {
		return nil, reqErr.response()
	}
	redirect := func(params url.Values) (*struct{ Body oauthRedirectResponse }, error) {
		return &struct{ Body oauthRedirectResponse }{Body: oauthRedirectResponse{RedirectURL: req.redirectURL(params)}}, nil
	}

	if !body.Approve {
		return redirect(url.Values{
			"error":             {"access_denied"},
			"error_description": {"the user denied the request"},
		})
	}

	perms, err := permissionsFor(c)
	if err != nil {
		return nil, err
	}
	if !perms.CanAny(models.ResourceAPIToken, models.ActionCreate) {
		return nil, huma.Error403Forbidden("you don't have the permission to create api tokens")
	}
	if len(body.Scopes) == 0 {
		return nil, huma.Error400BadRequest("at least one scope is required")
	}
	for _, scope := range body.Scopes {
		if !slices.Contains(req.scopes, scope) {
			return nil, huma.Error400BadRequest("the client didn't ask for the scope " + string(scope))
		}
	}
	scopes, err := normalizeAPITokenScopes(body.Scopes)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if scope, ungranted := ungrantedAPITokenScope(perms, scopes); ungranted {
		return nil, huma.Error403Forbidden("you don't have the permission for scope " + string(scope))
	}

	// Checked here too, so the user isn't sent back to a client that then
	// fails to get its token.
	tokens, err := gorm.G[models.APIToken](db.DB).Where("user_id = ?", user.Id).Find(ctx)
	if err != nil {
		return nil, internalError()
	}
	if kept, _ := replacedAPITokens(tokens, req.clientId); len(kept) >= maxAPITokensPerUser {
		return nil, huma.Error409Conflict(fmt.Sprintf("you can't have more than %d api tokens, revoke one first", maxAPITokensPerUser))
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
		return nil, huma.Error503ServiceUnavailable("too many pending authorizations, try again in a minute")
	}
	return redirect(url.Values{"code": {code}})
})

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
	var redeemedOfUser []string
	for key, existing := range s.codes {
		switch {
		case existing.isGone(now):
			delete(s.codes, key)
		case existing.userId != entry.userId:
		case existing.redeemed:
			redeemedOfUser = append(redeemedOfUser, key)
		default:
			pendingOfUser++
		}
	}
	if pendingOfUser >= maxOAuthCodesPerUser {
		return false
	}
	// Exchanged codes are only kept to notice when they are used again. They
	// count towards the user's limit too, oldest out first, so authorizing
	// again and again can't fill the store for everyone.
	slices.SortFunc(redeemedOfUser, func(a, b string) int {
		return s.codes[a].expiresAt.Compare(s.codes[b].expiresAt)
	})
	for _, key := range redeemedOfUser[:max(0, pendingOfUser+len(redeemedOfUser)+1-maxOAuthCodesPerUser)] {
		delete(s.codes, key)
	}
	if len(s.codes) >= maxOAuthCodes {
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
