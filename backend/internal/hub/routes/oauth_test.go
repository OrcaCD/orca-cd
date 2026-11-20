package routes

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/apitoken"
	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	testOAuthIssuer   = "https://hub.example.com"
	testOAuthRedirect = "http://127.0.0.1:33418/callback"
	testOAuthVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
)

func setupOAuthTest(t *testing.T) models.User {
	t.Helper()
	setupAPITokensTestDB(t)
	if err := InitOAuth("test-secret-that-is-long-enough-32chars", testOAuthIssuer); err != nil {
		t.Fatalf("InitOAuth() error: %v", err)
	}
	oauthCodes.mu.Lock()
	oauthCodes.codes = map[string]*oauthAuthorizationCode{}
	oauthCodes.mu.Unlock()
	t.Cleanup(func() {
		oauthIssuer, oauthClientKey, oauthNow = "", nil, time.Now
	})
	return createTestUser(t, "Alex", "alex@example.com", rbac.AdminRoleID, "")
}

// newOAuthRouter registers the OAuth routes like RegisterRoutes does. The
// consent routes run as user, the client routes without a user.
func newOAuthRouter(user *models.User, perms *rbac.Permissions) *gin.Engine {
	router := gin.New()
	router.POST("/api/v1/oauth/register", OAuthRegisterHandler)
	router.POST("/api/v1/oauth/token", OAuthTokenHandler)
	session := router.Group("", func(c *gin.Context) {
		auth.SetUser(c, user)
		if perms != nil {
			rbac.Set(c, perms)
		}
		c.Next()
	})
	session.GET("/api/v1/oauth/authorize", OAuthAuthorizationRequestHandler)
	session.POST("/api/v1/oauth/authorize", OAuthAuthorizeHandler)
	return router
}

func serveOAuth(router *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func registerOAuthClient(t *testing.T, router *gin.Engine, name string, redirectURIs ...string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"client_name": name, "redirect_uris": redirectURIs})
	w := serveOAuth(router, httptest.NewRequest(http.MethodPost, "/api/v1/oauth/register", strings.NewReader(string(body))))
	if w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp oauthClientRegistrationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("register: invalid response: %v", err)
	}
	return resp.ClientId
}

func codeChallengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func authorizationQuery(clientId string) url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {clientId},
		"redirect_uri":          {testOAuthRedirect},
		"code_challenge":        {codeChallengeOf(testOAuthVerifier)},
		"code_challenge_method": {"S256"},
		"state":                 {"xyz"},
		"resource":              {testOAuthIssuer + "/api/v1/mcp"},
	}
}

// approveOAuth answers the consent page and returns the redirect URL.
func approveOAuth(t *testing.T, router *gin.Engine, query url.Values, approve bool, scopes ...string) (*httptest.ResponseRecorder, *url.URL) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"query": query.Encode(), "approve": approve, "scopes": scopes})
	w := serveOAuth(router, httptest.NewRequest(http.MethodPost, "/api/v1/oauth/authorize", strings.NewReader(string(body))))
	var resp struct {
		RedirectUrl string `json:"redirectUrl"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	redirect, err := url.Parse(resp.RedirectUrl)
	if err != nil {
		t.Fatalf("invalid redirect URL %q: %v", resp.RedirectUrl, err)
	}
	return w, redirect
}

func requestOAuthToken(router *gin.Engine, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return serveOAuth(router, req)
}

func codeForm(clientId, code string) url.Values {
	return url.Values{
		"grant_type":    {grantTypeAuthorizationCode},
		"client_id":     {clientId},
		"code":          {code},
		"redirect_uri":  {testOAuthRedirect},
		"code_verifier": {testOAuthVerifier},
	}
}

func decodeOAuthToken(t *testing.T, w *httptest.ResponseRecorder) oauthTokenResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("token: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp oauthTokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("token: invalid response: %v", err)
	}
	return resp
}

func oauthErrorCode(w *httptest.ResponseRecorder) string {
	var resp struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return resp.Error
}

// authorizeOAuthClient registers a client, approves it and exchanges the code.
func authorizeOAuthClient(t *testing.T, router *gin.Engine, name string, scopes ...string) (string, oauthTokenResponse) {
	t.Helper()
	clientId := registerOAuthClient(t, router, name, testOAuthRedirect)
	w, redirect := approveOAuth(t, router, authorizationQuery(clientId), true, scopes...)
	if w.Code != http.StatusOK {
		t.Fatalf("authorize: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	return clientId, decodeOAuthToken(t, requestOAuthToken(router, codeForm(clientId, redirect.Query().Get("code"))))
}

func findOAuthToken(t *testing.T, accessToken string) (models.APIToken, bool) {
	t.Helper()
	token, err := gorm.G[models.APIToken](db.DB).Where("token_hash = ?", apitoken.Hash(accessToken)).First(t.Context())
	return token, err == nil
}

func TestOAuthFlow_IssuesScopedTokenAndRefreshesIt(t *testing.T) {
	user := setupOAuthTest(t)
	router := newOAuthRouter(&user, nil)

	clientId := registerOAuthClient(t, router, "Claude Code", testOAuthRedirect)
	query := authorizationQuery(clientId)

	// The consent page shows the client and what it asks for.
	w := serveOAuth(router, httptest.NewRequest(http.MethodGet, "/api/v1/oauth/authorize?"+query.Encode(), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("authorization request: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var page oauthAuthorizationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("invalid response: %v", err)
	}
	if page.ClientName != "Claude Code" || page.RedirectURI != testOAuthRedirect || !page.CanAuthorize || len(page.Scopes) != len(mcpScopes) {
		t.Errorf("unexpected consent page: %+v", page)
	}

	w, redirect := approveOAuth(t, router, query, true, "application:read")
	if w.Code != http.StatusOK {
		t.Fatalf("authorize: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := redirect.Scheme + "://" + redirect.Host + redirect.Path; got != testOAuthRedirect {
		t.Errorf("redirected to %q", got)
	}
	if redirect.Query().Get("state") != "xyz" || redirect.Query().Get("iss") != testOAuthIssuer {
		t.Errorf("redirect is missing state or issuer: %s", redirect)
	}

	resp := decodeOAuthToken(t, requestOAuthToken(router, codeForm(clientId, redirect.Query().Get("code"))))
	if resp.TokenType != "Bearer" || resp.Scope != "application:read" || resp.ExpiresIn <= 0 || resp.ExpiresIn > 3600 {
		t.Errorf("unexpected token response: %+v", resp)
	}
	token, ok := findOAuthToken(t, resp.AccessToken)
	if !ok {
		t.Fatal("the access token is not an API token")
	}
	if token.UserId != user.Id || token.Name != "Claude Code" || !token.IsOAuth() ||
		!slices.Equal(token.Scopes, []models.APITokenScope{"application:read"}) {
		t.Errorf("unexpected token: %+v", token)
	}

	refreshed := decodeOAuthToken(t, requestOAuthToken(router, url.Values{
		"grant_type":    {grantTypeRefreshToken},
		"client_id":     {clientId},
		"refresh_token": {resp.RefreshToken},
	}))
	if refreshed.AccessToken == resp.AccessToken || refreshed.RefreshToken == resp.RefreshToken {
		t.Error("refreshing must replace both tokens")
	}
	if _, ok := findOAuthToken(t, resp.AccessToken); ok {
		t.Error("the old access token still works")
	}
	if renewed, ok := findOAuthToken(t, refreshed.AccessToken); !ok || renewed.Id != token.Id {
		t.Error("the grant keeps its token row")
	}

	// The list shows when the grant ends, not the hourly access token.
	w = serveAPITokenRequest(t, &user, http.MethodGet, "/api/v1/api-tokens", nil)
	var listed []apiTokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || len(listed) != 1 {
		t.Fatalf("unexpected token list %s: %v", w.Body.String(), err)
	}
	if expiresAt, _ := time.Parse(time.RFC3339, listed[0].ExpiresAt); !listed[0].OAuth || time.Until(expiresAt) < oauthRefreshTokenLifetime-time.Minute {
		t.Errorf("unexpected listed token: %+v", listed[0])
	}
}

func TestOAuthRegister_RejectsInvalidClients(t *testing.T) {
	setupOAuthTest(t)
	router := newOAuthRouter(nil, nil)

	tests := []struct {
		name string
		body string
	}{
		{"no redirect", `{"client_name":"x","redirect_uris":[]}`},
		{"javascript", `{"redirect_uris":["javascript:alert(1)"]}`},
		{"http not on loopback", `{"redirect_uris":["http://evil.example/callback"]}`},
		{"fragment", `{"redirect_uris":["https://app.example/callback#x"]}`},
		{"too many", `{"redirect_uris":["https://a.example","https://b.example","https://c.example","https://d.example","https://e.example","https://f.example"]}`},
		{"implicit grant", `{"redirect_uris":["https://a.example"],"grant_types":["implicit"]}`},
		{"token response", `{"redirect_uris":["https://a.example"],"response_types":["token"]}`},
		{"not json", `redirect_uris=https://a.example`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serveOAuth(router, httptest.NewRequest(http.MethodPost, "/api/v1/oauth/register", strings.NewReader(tt.body)))
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestIsValidRedirectURI(t *testing.T) {
	tests := []struct {
		uri  string
		want bool
	}{
		{"https://claude.ai/api/mcp/auth_callback", true},
		{"http://localhost:8080/callback", true},
		{"http://127.0.0.1:33418", true},
		{"http://[::1]:1234/cb", true},
		{"cursor://anysphere.cursor-retrieval/oauth/callback", true},
		{"com.example.app:/oauth", true},
		{"http://example.com/callback", false},
		{"http://localhost.evil.example/callback", false},
		{"https://user@app.example/callback", false},
		{"https:///callback", false},
		{"javascript:alert(1)", false},
		{"JavaScript://%0aalert(1)", false},
		{"data:text/html,x", false},
		{"ms-officecmd://x", false},
		{"intent://x#Intent;end", false},
		{"search-ms://query=x", false},
		{"myapp:callback", false},
		{"/relative", false},
		{"https://app.example/" + strings.Repeat("a", 512), false},
	}
	for _, tt := range tests {
		if got := isValidRedirectURI(tt.uri); got != tt.want {
			t.Errorf("isValidRedirectURI(%q) = %v, want %v", tt.uri, got, tt.want)
		}
	}
}

func TestOAuthClientId_IsSigned(t *testing.T) {
	setupOAuthTest(t)
	clientId := encodeOAuthClientId(&oauthClient{Name: "App", RedirectURIs: []string{"https://app.example/cb"}, Nonce: "n"})

	if client, ok := decodeOAuthClientId(clientId); !ok || client.Name != "App" {
		t.Fatalf("decodeOAuthClientId() = %+v, %v", client, ok)
	}

	signed, signature, _ := strings.Cut(clientId, ".")
	forged := encodeOAuthClientId(&oauthClient{Name: "App", RedirectURIs: []string{"https://evil.example/cb"}, Nonce: "n"})
	forgedSigned, _, _ := strings.Cut(forged, ".")
	// The last character of the signature carries unused bits, a lenient
	// decoder would accept it changed.
	respelled := []byte(signature)
	respelled[len(respelled)-1] ^= 1
	for name, id := range map[string]string{
		"other redirect": forgedSigned + "." + signature,
		"respelled":      signed + "." + string(respelled),
		"no signature":   signed,
		"empty":          "",
	} {
		if _, ok := decodeOAuthClientId(id); ok {
			t.Errorf("%s: accepted a client ID that wasn't signed", name)
		}
	}

	if err := InitOAuth("another-secret-that-is-long-enough-32", testOAuthIssuer); err != nil {
		t.Fatal(err)
	}
	if _, ok := decodeOAuthClientId(clientId); ok {
		t.Error("a client ID of another hub was accepted")
	}
}

func TestOAuthClientName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"Claude Code", "Claude Code"},
		{"  Claude \n\t Code ", "Claude Code"},
		{"Evil\u202eedoC", "EviledoC"},
		{"Zero\u200bWidth", "ZeroWidth"},
		{"", defaultOAuthClientName},
		{"\x00\x01", defaultOAuthClientName},
		{strings.Repeat("a", 100), strings.Repeat("a", maxOAuthClientNameLength)},
	}
	for _, tt := range tests {
		if got := oauthClientName(tt.name); got != tt.want {
			t.Errorf("oauthClientName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestOAuthClientRedirectURI(t *testing.T) {
	client := oauthClient{RedirectURIs: []string{"http://localhost:1234/callback", "https://app.example/cb"}}

	tests := []struct {
		requested string
		want      string
		ok        bool
	}{
		{"https://app.example/cb", "https://app.example/cb", true},
		// Native apps get a new port each time.
		{"http://localhost:5678/callback", "http://localhost:5678/callback", true},
		{"http://localhost:5678/other", "", false},
		{"http://127.0.0.1:1234/callback", "", false},
		{"https://app.example/cb/", "", false},
		{"https://app.example/cb?x=1", "", false},
		// The client has more than one, so it has to name it.
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := client.redirectURI(tt.requested)
		if got != tt.want || ok != tt.ok {
			t.Errorf("redirectURI(%q) = %q, %v, want %q, %v", tt.requested, got, ok, tt.want, tt.ok)
		}
	}

	single := oauthClient{RedirectURIs: []string{"https://app.example/cb"}}
	if got, ok := single.redirectURI(""); !ok || got != "https://app.example/cb" {
		t.Errorf("a client with one redirect URI should not have to name it, got %q, %v", got, ok)
	}
}

func TestParseOAuthAuthorizationRequest_Errors(t *testing.T) {
	setupOAuthTest(t)
	clientId := encodeOAuthClientId(&oauthClient{Name: "App", RedirectURIs: []string{testOAuthRedirect}, Nonce: "n"})

	tests := []struct {
		name       string
		change     func(url.Values)
		code       string
		redirected bool
	}{
		{"unknown client", func(q url.Values) { q.Set("client_id", "mcp_x.y") }, "invalid_client", false},
		{"other redirect", func(q url.Values) { q.Set("redirect_uri", "https://evil.example/cb") }, "invalid_request", false},
		{"repeated parameter", func(q url.Values) { q.Add("state", "again") }, "invalid_request", false},
		{"token response", func(q url.Values) { q.Set("response_type", "token") }, "unsupported_response_type", true},
		{"no PKCE", func(q url.Values) { q.Del("code_challenge") }, "invalid_request", true},
		{"plain PKCE", func(q url.Values) { q.Set("code_challenge_method", "plain") }, "invalid_request", true},
		{"short challenge", func(q url.Values) { q.Set("code_challenge", "abc") }, "invalid_request", true},
		{"other resource", func(q url.Values) { q.Set("resource", "https://other.example/mcp") }, "invalid_target", true},
		{"admin scope", func(q url.Values) { q.Set("scope", "admin") }, "invalid_scope", true},
		{"unknown scope", func(q url.Values) { q.Set("scope", "cluster:read") }, "invalid_scope", true},
		{"scope outside MCP", func(q url.Values) { q.Set("scope", "user:delete") }, "invalid_scope", true},
		{"long state", func(q url.Values) { q.Set("state", strings.Repeat("s", 2000)) }, "invalid_request", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := authorizationQuery(clientId)
			tt.change(query)
			_, err := parseOAuthAuthorizationRequest(query)
			if err == nil {
				t.Fatal("expected an error")
			}
			if err.code != tt.code {
				t.Errorf("code = %q, want %q", err.code, tt.code)
			}
			if (err.redirectURL != "") != tt.redirected {
				t.Errorf("redirectURL = %q, want redirected = %v", err.redirectURL, tt.redirected)
			}
			if tt.redirected && !strings.HasPrefix(err.redirectURL, testOAuthRedirect+"?") {
				t.Errorf("redirected to %q", err.redirectURL)
			}
		})
	}

	// Without a scope, the client gets the scopes of the MCP tools.
	query := authorizationQuery(clientId)
	query.Del("resource")
	req, err := parseOAuthAuthorizationRequest(query)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if !slices.Equal(req.scopes, mcpScopes) {
		t.Errorf("scopes = %v, want %v", req.scopes, mcpScopes)
	}

	// Scopes other than the ones of MCP, like offline_access, are left out.
	query.Set("scope", "offline_access application:read openid user:delete")
	req, err = parseOAuthAuthorizationRequest(query)
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if want := []models.APITokenScope{"application:read"}; !slices.Equal(req.scopes, want) {
		t.Errorf("scopes = %v, want %v", req.scopes, want)
	}
}

func TestOAuthAuthorize_Consent(t *testing.T) {
	user := setupOAuthTest(t)
	clientId := encodeOAuthClientId(&oauthClient{Name: "App", RedirectURIs: []string{testOAuthRedirect}, Nonce: "n"})
	readOnly := rbac.New([]models.RolePermission{
		{Resource: models.ResourceApplication, Action: models.ActionRead},
		{Resource: models.ResourceAPIToken, Action: models.ActionCreate},
	})

	t.Run("denied", func(t *testing.T) {
		w, redirect := approveOAuth(t, newOAuthRouter(&user, nil), authorizationQuery(clientId), false)
		if w.Code != http.StatusOK || redirect.Query().Get("error") != "access_denied" || redirect.Query().Get("code") != "" {
			t.Errorf("expected a redirect with access_denied, got %d: %s", w.Code, w.Body.String())
		}
	})
	t.Run("scope not asked for", func(t *testing.T) {
		query := authorizationQuery(clientId)
		query.Set("scope", "application:read")
		if w, _ := approveOAuth(t, newOAuthRouter(&user, nil), query, true, "application:deploy"); w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
	t.Run("no scope", func(t *testing.T) {
		if w, _ := approveOAuth(t, newOAuthRouter(&user, nil), authorizationQuery(clientId), true); w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
	t.Run("scope the user doesn't have", func(t *testing.T) {
		if w, _ := approveOAuth(t, newOAuthRouter(&user, readOnly), authorizationQuery(clientId), true, "application:deploy"); w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
	})
	t.Run("may not create tokens", func(t *testing.T) {
		perms := rbac.New([]models.RolePermission{{Resource: models.ResourceApplication, Action: models.ActionRead}})
		router := newOAuthRouter(&user, perms)
		if w, _ := approveOAuth(t, router, authorizationQuery(clientId), true, "application:read"); w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
		w := serveOAuth(router, httptest.NewRequest(http.MethodGet, "/api/v1/oauth/authorize?"+authorizationQuery(clientId).Encode(), nil))
		var page oauthAuthorizationResponse
		_ = json.Unmarshal(w.Body.Bytes(), &page)
		if w.Code != http.StatusOK || page.CanAuthorize {
			t.Errorf("expected the consent page to say so, got %d: %s", w.Code, w.Body.String())
		}
	})
	t.Run("shows which scopes the user can give", func(t *testing.T) {
		w := serveOAuth(newOAuthRouter(&user, readOnly), httptest.NewRequest(http.MethodGet, "/api/v1/oauth/authorize?"+authorizationQuery(clientId).Encode(), nil))
		var page oauthAuthorizationResponse
		_ = json.Unmarshal(w.Body.Bytes(), &page)
		want := []oauthScopeResponse{{"application:read", true}, {"agent:read", false}, {"application:deploy", false}}
		if !slices.Equal(page.Scopes, want) {
			t.Errorf("scopes = %+v, want %+v", page.Scopes, want)
		}
	})
}

func TestOAuthToken_RejectsInvalidCodeExchanges(t *testing.T) {
	user := setupOAuthTest(t)
	router := newOAuthRouter(&user, nil)
	clientId := registerOAuthClient(t, router, "App", testOAuthRedirect)
	otherClientId := registerOAuthClient(t, router, "Other", testOAuthRedirect)

	tests := []struct {
		name   string
		change func(url.Values)
	}{
		{"wrong verifier", func(f url.Values) { f.Set("code_verifier", strings.Repeat("a", 43)) }},
		{"no verifier", func(f url.Values) { f.Del("code_verifier") }},
		{"other client", func(f url.Values) { f.Set("client_id", otherClientId) }},
		{"other redirect", func(f url.Values) { f.Set("redirect_uri", "http://127.0.0.1:1/callback") }},
		{"no redirect", func(f url.Values) { f.Del("redirect_uri") }},
		{"unknown code", func(f url.Values) { f.Set("code", "unknown") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, redirect := approveOAuth(t, router, authorizationQuery(clientId), true, "application:read")
			form := codeForm(clientId, redirect.Query().Get("code"))
			tt.change(form)
			if w := requestOAuthToken(router, form); w.Code != http.StatusBadRequest || oauthErrorCode(w) != "invalid_grant" {
				t.Errorf("expected invalid_grant, got %d: %s", w.Code, w.Body.String())
			}
			// A code can only be tried once.
			if tt.name == "unknown code" {
				return
			}
			if w := requestOAuthToken(router, codeForm(clientId, redirect.Query().Get("code"))); w.Code != http.StatusBadRequest {
				t.Errorf("the code could be used after a failed exchange: %d", w.Code)
			}
		})
	}

	t.Run("expired code", func(t *testing.T) {
		_, redirect := approveOAuth(t, router, authorizationQuery(clientId), true, "application:read")
		oauthNow = func() time.Time { return time.Now().Add(oauthCodeLifetime + time.Second) }
		t.Cleanup(func() { oauthNow = time.Now })
		if w := requestOAuthToken(router, codeForm(clientId, redirect.Query().Get("code"))); oauthErrorCode(w) != "invalid_grant" {
			t.Errorf("expected invalid_grant, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestOAuthToken_CodeUsedTwiceRevokesToken(t *testing.T) {
	user := setupOAuthTest(t)
	router := newOAuthRouter(&user, nil)
	clientId := registerOAuthClient(t, router, "App", testOAuthRedirect)
	_, redirect := approveOAuth(t, router, authorizationQuery(clientId), true, "application:read")
	form := codeForm(clientId, redirect.Query().Get("code"))

	resp := decodeOAuthToken(t, requestOAuthToken(router, form))
	if w := requestOAuthToken(router, form); oauthErrorCode(w) != "invalid_grant" {
		t.Fatalf("expected invalid_grant, got %d: %s", w.Code, w.Body.String())
	}
	if _, ok := findOAuthToken(t, resp.AccessToken); ok {
		t.Error("the token of a code that was used twice still works")
	}
}

func TestOAuthToken_Refresh(t *testing.T) {
	user := setupOAuthTest(t)
	router := newOAuthRouter(&user, nil)
	refresh := func(clientId, refreshToken, scope string) *httptest.ResponseRecorder {
		form := url.Values{"grant_type": {grantTypeRefreshToken}, "client_id": {clientId}, "refresh_token": {refreshToken}}
		if scope != "" {
			form.Set("scope", scope)
		}
		return requestOAuthToken(router, form)
	}

	t.Run("other client", func(t *testing.T) {
		_, resp := authorizeOAuthClient(t, router, "App", "application:read")
		otherClientId := registerOAuthClient(t, router, "Other", testOAuthRedirect)
		if w := refresh(otherClientId, resp.RefreshToken, ""); oauthErrorCode(w) != "invalid_grant" {
			t.Errorf("expected invalid_grant, got %d: %s", w.Code, w.Body.String())
		}
	})
	t.Run("access token is no refresh token", func(t *testing.T) {
		clientId, resp := authorizeOAuthClient(t, router, "App", "application:read")
		if w := refresh(clientId, resp.AccessToken, ""); oauthErrorCode(w) != "invalid_grant" {
			t.Errorf("expected invalid_grant, got %d: %s", w.Code, w.Body.String())
		}
	})
	t.Run("narrows but never widens scopes", func(t *testing.T) {
		clientId, resp := authorizeOAuthClient(t, router, "App", "application:read", "agent:read")
		if w := refresh(clientId, resp.RefreshToken, "application:read application:deploy"); oauthErrorCode(w) != "invalid_scope" {
			t.Fatalf("expected invalid_scope, got %d: %s", w.Code, w.Body.String())
		}
		narrowed := decodeOAuthToken(t, refresh(clientId, resp.RefreshToken, "agent:read"))
		if narrowed.Scope != "agent:read" {
			t.Errorf("scope = %q, want agent:read", narrowed.Scope)
		}
	})
	t.Run("expired grant", func(t *testing.T) {
		clientId, resp := authorizeOAuthClient(t, router, "App", "application:read")
		oauthNow = func() time.Time { return time.Now().Add(oauthRefreshTokenLifetime + time.Minute) }
		t.Cleanup(func() { oauthNow = time.Now })
		if w := refresh(clientId, resp.RefreshToken, ""); oauthErrorCode(w) != "invalid_grant" {
			t.Errorf("expected invalid_grant, got %d: %s", w.Code, w.Body.String())
		}
	})
	t.Run("older refresh token revokes the grant", func(t *testing.T) {
		// Someone copied the first refresh token and refreshed twice with it,
		// before the client uses it.
		clientId, first := authorizeOAuthClient(t, router, "App", "application:read")
		second := decodeOAuthToken(t, refresh(clientId, first.RefreshToken, ""))
		third := decodeOAuthToken(t, refresh(clientId, second.RefreshToken, ""))
		if w := refresh(clientId, first.RefreshToken, ""); oauthErrorCode(w) != "invalid_grant" {
			t.Fatalf("expected invalid_grant, got %d: %s", w.Code, w.Body.String())
		}
		if _, ok := findOAuthToken(t, third.AccessToken); ok {
			t.Error("the grant still works after an old refresh token was used")
		}
	})
	t.Run("made up refresh token of a grant", func(t *testing.T) {
		clientId, resp := authorizeOAuthClient(t, router, "App", "application:read")
		grantId, _, _ := strings.Cut(resp.RefreshToken, ".")
		if w := refresh(clientId, grantId+".guessed", ""); oauthErrorCode(w) != "invalid_grant" {
			t.Fatalf("expected invalid_grant, got %d: %s", w.Code, w.Body.String())
		}
		// Only a refresh token the grant had revokes it.
		if _, ok := findOAuthToken(t, resp.AccessToken); !ok {
			t.Error("a made up refresh token revoked the grant")
		}
	})
	t.Run("reused refresh token revokes the grant", func(t *testing.T) {
		clientId, resp := authorizeOAuthClient(t, router, "App", "application:read")
		refreshed := decodeOAuthToken(t, refresh(clientId, resp.RefreshToken, ""))
		oauthNow = func() time.Time { return time.Now().Add(oauthRefreshReuseGrace + time.Second) }
		t.Cleanup(func() { oauthNow = time.Now })
		if w := refresh(clientId, resp.RefreshToken, ""); oauthErrorCode(w) != "invalid_grant" {
			t.Fatalf("expected invalid_grant, got %d: %s", w.Code, w.Body.String())
		}
		if _, ok := findOAuthToken(t, refreshed.AccessToken); ok {
			t.Error("the grant still works after its old refresh token was used")
		}
		if w := refresh(clientId, refreshed.RefreshToken, ""); oauthErrorCode(w) != "invalid_grant" {
			t.Errorf("the current refresh token still works: %d", w.Code)
		}
	})
	t.Run("concurrent refreshes", func(t *testing.T) {
		clientId, resp := authorizeOAuthClient(t, router, "App", "application:read")
		var mu sync.Mutex
		succeeded := 0
		var wg sync.WaitGroup
		for range 5 {
			wg.Go(func() {
				if w := refresh(clientId, resp.RefreshToken, ""); w.Code == http.StatusOK {
					mu.Lock()
					succeeded++
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		if succeeded != 1 {
			t.Errorf("%d refreshes succeeded, want 1", succeeded)
		}
		// The client refreshed twice at once, the grant keeps working.
		grantId, _, _ := strings.Cut(resp.RefreshToken, ".")
		if token, err := gorm.G[models.APIToken](db.DB).Where("id = ?", grantId).First(t.Context()); err != nil {
			t.Errorf("the grant was revoked: %v", err)
		} else if token.RefreshedAt == nil || len(token.UsedRefreshTokenHashes) != 1 {
			t.Errorf("unexpected grant after refresh: %+v", token)
		}
	})
}

func TestOAuthToken_GrantLimits(t *testing.T) {
	user := setupOAuthTest(t)
	router := newOAuthRouter(&user, nil)

	t.Run("authorizing a client again replaces its grant", func(t *testing.T) {
		clientId, first := authorizeOAuthClient(t, router, "Cursor", "application:read")
		_, redirect := approveOAuth(t, router, authorizationQuery(clientId), true, "agent:read")
		second := decodeOAuthToken(t, requestOAuthToken(router, codeForm(clientId, redirect.Query().Get("code"))))
		if _, ok := findOAuthToken(t, first.AccessToken); ok {
			t.Error("the first grant still works")
		}
		if token, ok := findOAuthToken(t, second.AccessToken); !ok || token.Name != "Cursor" {
			t.Errorf("unexpected token: %+v", token)
		}
	})
	t.Run("ended grants are deleted", func(t *testing.T) {
		_, ended := authorizeOAuthClient(t, router, "Old", "application:read")
		oauthNow = func() time.Time { return time.Now().Add(oauthRefreshTokenLifetime + time.Minute) }
		t.Cleanup(func() { oauthNow = time.Now })
		authorizeOAuthClient(t, router, "New", "application:read")
		grantId, _, _ := strings.Cut(ended.RefreshToken, ".")
		if count, _ := gorm.G[models.APIToken](db.DB).Where("id = ?", grantId).Count(t.Context(), "*"); count != 0 {
			t.Error("the ended grant is still stored")
		}
		oauthNow = time.Now
	})
	t.Run("names are unique", func(t *testing.T) {
		_, first := authorizeOAuthClient(t, router, "VS Code", "application:read")
		_, second := authorizeOAuthClient(t, router, "vs code", "application:read")
		a, _ := findOAuthToken(t, first.AccessToken)
		b, _ := findOAuthToken(t, second.AccessToken)
		if a.Name != "VS Code" || b.Name != "vs code (2)" {
			t.Errorf("names = %q, %q", a.Name, b.Name)
		}
	})
	t.Run("too many tokens", func(t *testing.T) {
		clientId := registerOAuthClient(t, router, "Late", testOAuthRedirect)
		_, redirect := approveOAuth(t, router, authorizationQuery(clientId), true, "application:read")
		count, _ := gorm.G[models.APIToken](db.DB).Where("user_id = ?", user.Id).Count(t.Context(), "*")
		for i := range maxAPITokensPerUser - int(count) {
			generated := apitoken.Generate()
			if err := gorm.G[models.APIToken](db.DB).Create(t.Context(), &models.APIToken{
				UserId: user.Id, Name: "token " + strings.Repeat("x", i+1), TokenHash: generated.Hash, Prefix: generated.Prefix,
				Scopes: []models.APITokenScope{"application:read"}, ExpiresAt: time.Now().Add(time.Hour),
			}); err != nil {
				t.Fatal(err)
			}
		}
		if w := requestOAuthToken(router, codeForm(clientId, redirect.Query().Get("code"))); oauthErrorCode(w) != "invalid_grant" {
			t.Errorf("expected invalid_grant, got %d: %s", w.Code, w.Body.String())
		}
		if w, _ := approveOAuth(t, router, authorizationQuery(clientId), true, "application:read"); w.Code != http.StatusConflict {
			t.Errorf("expected the consent page to refuse, got %d", w.Code)
		}
	})
}

func TestOAuthAuthorize_LimitsPendingCodesPerUser(t *testing.T) {
	user := setupOAuthTest(t)
	other := createTestUser(t, "Other", "other@example.com", rbac.AdminRoleID, "")
	clientId := encodeOAuthClientId(&oauthClient{Name: "App", RedirectURIs: []string{testOAuthRedirect}, Nonce: "n"})

	for i := range maxOAuthCodesPerUser {
		if w, _ := approveOAuth(t, newOAuthRouter(&user, nil), authorizationQuery(clientId), true, "application:read"); w.Code != http.StatusOK {
			t.Fatalf("code %d: expected 200, got %d", i, w.Code)
		}
	}
	if w, _ := approveOAuth(t, newOAuthRouter(&user, nil), authorizationQuery(clientId), true, "application:read"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 once the user has %d pending codes, got %d", maxOAuthCodesPerUser, w.Code)
	}
	if w, _ := approveOAuth(t, newOAuthRouter(&other, nil), authorizationQuery(clientId), true, "application:read"); w.Code != http.StatusOK {
		t.Errorf("another user was blocked: %d", w.Code)
	}
}

func TestOAuthToken_RejectsOtherRequests(t *testing.T) {
	setupOAuthTest(t)
	router := newOAuthRouter(nil, nil)

	if w := requestOAuthToken(router, url.Values{"grant_type": {"client_credentials"}}); oauthErrorCode(w) != "unsupported_grant_type" {
		t.Errorf("expected unsupported_grant_type, got %d: %s", w.Code, w.Body.String())
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/oauth/token", strings.NewReader("grant_type=refresh_token&grant_type=authorization_code"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if w := serveOAuth(router, req); oauthErrorCode(w) != "invalid_request" {
		t.Errorf("expected invalid_request for a repeated parameter, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCodeChallenge(t *testing.T) {
	challenge := codeChallengeOf(testOAuthVerifier)
	if !verifyCodeChallenge(testOAuthVerifier, challenge) {
		t.Error("the right verifier was rejected")
	}
	for _, verifier := range []string{"", "short", testOAuthVerifier + "x", strings.Repeat("a", 129), strings.Repeat("ä", 43)} {
		if verifyCodeChallenge(verifier, challenge) {
			t.Errorf("verifier %q was accepted", verifier)
		}
	}
}
