package routes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/apitoken"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"gorm.io/gorm"
)

// clientMetadataServer serves metadata documents over TLS, like a client's
// website. The hub fetches from it with a client that trusts its certificate,
// as the real one refuses local addresses.
type clientMetadataServer struct {
	*httptest.Server
	requests atomic.Int32
	handlers map[string]http.HandlerFunc
}

func newClientMetadataServer(t *testing.T) *clientMetadataServer {
	t.Helper()
	s := &clientMetadataServer{handlers: map[string]http.HandlerFunc{}}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		if handler, ok := s.handlers[r.URL.Path]; ok {
			handler(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(s.Close)

	client := s.Client()
	client.Timeout = oauthClientMetadataTimeout
	client.CheckRedirect = oauthClientMetadataHTTPClient.CheckRedirect
	previous := oauthClientMetadataHTTPClient
	oauthClientMetadataHTTPClient = client
	oauthClientMetadataCache.mu.Lock()
	oauthClientMetadataCache.entries = map[string]*cachedClientMetadata{}
	oauthClientMetadataCache.mu.Unlock()
	t.Cleanup(func() { oauthClientMetadataHTTPClient = previous })
	return s
}

// serve publishes a document at path and returns its client ID.
func (s *clientMetadataServer) serve(path string, document map[string]any, headers ...string) string {
	clientId := s.URL + path
	if _, ok := document["client_id"]; !ok {
		document["client_id"] = clientId
	}
	body, _ := json.Marshal(document)
	s.handlers[path] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for i := 0; i+1 < len(headers); i += 2 {
			w.Header().Set(headers[i], headers[i+1])
		}
		_, _ = w.Write(body)
	}
	return clientId
}

func validClientMetadata() map[string]any {
	return map[string]any{
		"client_name":   "Claude Code",
		"redirect_uris": []string{testOAuthRedirect},
		"grant_types":   []string{"authorization_code", "refresh_token"},
	}
}

func TestValidateOAuthClientMetadataURL(t *testing.T) {
	setupOAuthTest(t)
	tests := []struct {
		clientId string
		valid    bool
	}{
		{"https://claude.ai/oauth/claude-code-client-metadata", true},
		{"https://app.example:8443/client.json", true},
		{"https://app.example/a%20b/client", true},
		{"http://app.example/client", false},
		{"https://app.example", false},
		{"https://app.example/", false},
		{"https://app.example/client?x=1", false},
		{"https://app.example/client#x", false},
		{"https://user:pass@app.example/client", false},
		{"https://app.example/a/../client", false},
		{"https://app.example/a/./client", false},
		{"https://app.example/a/%2e%2e/client", false},
		{"https:app.example/client", false},
		{"https://app.example/" + strings.Repeat("a", maxOAuthClientMetadataURLLength), false},
		// The hub itself
		{testOAuthIssuer + "/client", false},
		{"https://HUB.example.com/client", false},
	}
	for _, tt := range tests {
		if err := validateOAuthClientMetadataURL(tt.clientId); (err == nil) != tt.valid {
			t.Errorf("validateOAuthClientMetadataURL(%q) = %v, want valid %v", tt.clientId, err, tt.valid)
		}
	}
}

func TestOAuthClientMetadataDocument(t *testing.T) {
	const clientId = "https://app.example/client"
	valid := func() oauthClientMetadataDocument {
		return oauthClientMetadataDocument{ClientId: clientId, ClientName: "App", RedirectURIs: []string{testOAuthRedirect}}
	}

	t.Run("valid", func(t *testing.T) {
		document := valid()
		document.RedirectURIs = append(document.RedirectURIs, "javascript:alert(1)", testOAuthRedirect, "https://app.example/callback")
		document.TokenEndpointAuthMethodsSupported = []string{"private_key_jwt", "none"}
		client, err := document.client(clientId)
		if err != nil {
			t.Fatalf("client() error = %v", err)
		}
		// Unusable redirect URIs are dropped, the others kept.
		if client.Name != "App" || len(client.RedirectURIs) != 2 || client.RedirectURIs[1] != "https://app.example/callback" {
			t.Errorf("unexpected client %+v", client)
		}
	})
	t.Run("name defaults to the host", func(t *testing.T) {
		document := valid()
		document.ClientName = "  "
		if client, err := document.client(clientId); err != nil || client.Name != "app.example" {
			t.Errorf("client() = %+v, %v", client, err)
		}
	})

	for name, change := range map[string]func(*oauthClientMetadataDocument){
		"other client_id":      func(d *oauthClientMetadataDocument) { d.ClientId = clientId + "/" },
		"no client_id":         func(d *oauthClientMetadataDocument) { d.ClientId = "" },
		"client secret":        func(d *oauthClientMetadataDocument) { d.ClientSecret = json.RawMessage(`"s3cret"`) },
		"client secret expiry": func(d *oauthClientMetadataDocument) { d.ClientSecretExpiresAt = json.RawMessage(`0`) },
		"secret auth method":   func(d *oauthClientMetadataDocument) { d.TokenEndpointAuthMethod = "client_secret_basic" },
		"only key auth methods": func(d *oauthClientMetadataDocument) {
			d.TokenEndpointAuthMethodsSupported = []string{"private_key_jwt"}
		},
		"method not in its list": func(d *oauthClientMetadataDocument) {
			d.TokenEndpointAuthMethodsSupported = []string{"none"}
			d.TokenEndpointAuthMethod = "private_key_jwt"
		},
		"no code grant":             func(d *oauthClientMetadataDocument) { d.GrantTypes = []string{"client_credentials"} },
		"no code response":          func(d *oauthClientMetadataDocument) { d.ResponseTypes = []string{"token"} },
		"no redirect URIs":          func(d *oauthClientMetadataDocument) { d.RedirectURIs = nil },
		"only unusable redirectURI": func(d *oauthClientMetadataDocument) { d.RedirectURIs = []string{"http://app.example/callback"} },
	} {
		t.Run(name, func(t *testing.T) {
			document := valid()
			change(&document)
			if _, err := document.client(clientId); !errors.Is(err, errOAuthClientMetadataInvalid) {
				t.Errorf("client() error = %v, want an invalid document", err)
			}
		})
	}
}

func TestClientMetadataCacheDuration(t *testing.T) {
	tests := map[string]time.Duration{
		"":                          oauthClientMetadataDefaultCache,
		"public, max-age=3600":      time.Hour,
		"max-age=0":                 oauthClientMetadataMinCache,
		"max-age=999999999999":      oauthClientMetadataMaxCache,
		`max-age="600"`:             10 * time.Minute,
		"max-age=abc":               oauthClientMetadataDefaultCache,
		"no-store":                  0,
		"max-age=3600, no-cache":    0,
		"Max-Age=120":               2 * time.Minute,
		"s-maxage=10, max-age=1200": 20 * time.Minute,
	}
	for header, want := range tests {
		if got := clientMetadataCacheDuration(header); got != want {
			t.Errorf("clientMetadataCacheDuration(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestFetchOAuthClientMetadata(t *testing.T) {
	setupOAuthTest(t)
	server := newClientMetadataServer(t)
	current := time.Now()
	oauthNow = func() time.Time { return current }

	t.Run("cached while fresh", func(t *testing.T) {
		clientId := server.serve("/cached", validClientMetadata(), "Cache-Control", "max-age=120")
		before := server.requests.Load()
		for range 3 {
			if client, err := fetchOAuthClientMetadata(t.Context(), clientId); err != nil || client.Name != "Claude Code" {
				t.Fatalf("fetch = %+v, %v", client, err)
			}
		}
		if got := server.requests.Load() - before; got != 1 {
			t.Errorf("fetched %d times, want once", got)
		}
		current = current.Add(3 * time.Minute)
		if _, err := fetchOAuthClientMetadata(t.Context(), clientId); err != nil {
			t.Fatal(err)
		}
		if got := server.requests.Load() - before; got != 2 {
			t.Errorf("fetched %d times, want again after max-age", got)
		}
	})

	t.Run("a stale document covers an outage", func(t *testing.T) {
		clientId := server.serve("/outage", validClientMetadata())
		if _, err := fetchOAuthClientMetadata(t.Context(), clientId); err != nil {
			t.Fatal(err)
		}
		server.handlers["/outage"] = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }
		current = current.Add(oauthClientMetadataDefaultCache + time.Minute)
		if _, err := fetchOAuthClientMetadata(t.Context(), clientId); err != nil {
			t.Errorf("expected the stale document during an outage, got %v", err)
		}
		current = current.Add(oauthClientMetadataMaxStale)
		if _, err := fetchOAuthClientMetadata(t.Context(), clientId); !errors.Is(err, errOAuthClientMetadataUnavailable) {
			t.Errorf("expected the document to be unavailable after a long outage, got %v", err)
		}
	})

	t.Run("an invalid document replaces the cached one", func(t *testing.T) {
		clientId := server.serve("/changed", validClientMetadata())
		if _, err := fetchOAuthClientMetadata(t.Context(), clientId); err != nil {
			t.Fatal(err)
		}
		document := validClientMetadata()
		document["client_secret"] = "s3cret"
		server.serve("/changed", document)
		current = current.Add(oauthClientMetadataDefaultCache + time.Minute)
		if _, err := fetchOAuthClientMetadata(t.Context(), clientId); !errors.Is(err, errOAuthClientMetadataInvalid) {
			t.Errorf("expected the invalid document to be refused, got %v", err)
		}
		if oauthClientMetadataCache.get(clientId) != nil {
			t.Error("the old document is still cached")
		}
	})

	t.Run("refused responses", func(t *testing.T) {
		valid := server.serve("/target", validClientMetadata())
		large := validClientMetadata()
		large["client_uri"] = "https://app.example/" + strings.Repeat("a", maxOAuthClientMetadataBytes)
		for name, handler := range map[string]http.HandlerFunc{
			"redirect":  func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, valid, http.StatusFound) },
			"not-found": http.NotFound,
			"html": func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<html></html>"))
			},
			"not-an-object": func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("[1, 2]"))
			},
			"too-large": func(w http.ResponseWriter, _ *http.Request) {
				body, _ := json.Marshal(large)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(body)
			},
		} {
			t.Run(name, func(t *testing.T) {
				server.handlers["/"+name] = handler
				if _, err := fetchOAuthClientMetadata(t.Context(), server.URL+"/"+name); !errors.Is(err, errOAuthClientMetadataInvalid) {
					t.Errorf("expected an invalid document, got %v", err)
				}
			})
		}
	})

	t.Run("served as a JSON subtype", func(t *testing.T) {
		clientId := server.serve("/subtype", validClientMetadata(), "Content-Type", "application/client-metadata+json; charset=utf-8")
		if _, err := fetchOAuthClientMetadata(t.Context(), clientId); err != nil {
			t.Errorf("fetch error = %v", err)
		}
	})
}

// The real client refuses local addresses, so a user can't make the hub
// reach into its own network.
func TestOAuthClientMetadataHTTPClient_RefusesLocalAddresses(t *testing.T) {
	setupOAuthTest(t)
	oauthClientMetadataCache.mu.Lock()
	oauthClientMetadataCache.entries = map[string]*cachedClientMetadata{}
	oauthClientMetadataCache.mu.Unlock()
	var reached atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Store(true) }))
	t.Cleanup(server.Close)

	_, err := fetchOAuthClientMetadata(t.Context(), server.URL+"/client")
	if !errors.Is(err, errOAuthClientMetadataUnavailable) || reached.Load() {
		t.Errorf("fetch error = %v, reached = %v, want the local address refused", err, reached.Load())
	}
	if strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("the error tells the user the address: %v", err)
	}
}

func TestOAuthFlow_ClientMetadataDocument(t *testing.T) {
	user := setupOAuthTest(t)
	server := newClientMetadataServer(t)
	clientId := server.serve("/claude-code", validClientMetadata())
	router := newOAuthRouter(&user, nil)

	query := authorizationQuery(clientId)
	w := serveOAuth(router, httptest.NewRequest(http.MethodGet, "/api/v1/oauth/authorize?"+query.Encode(), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("authorization request: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var request oauthAuthorizationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &request); err != nil {
		t.Fatal(err)
	}
	if request.ClientName != "Claude Code" || request.ClientHost != strings.TrimPrefix(server.URL, "https://") {
		t.Errorf("unexpected request %+v", request)
	}

	authorize := func() oauthTokenResponse {
		t.Helper()
		w, redirect := approveOAuth(t, router, query, true, "application:read")
		if w.Code != http.StatusOK {
			t.Fatalf("authorize: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		return decodeOAuthToken(t, requestOAuthToken(router, codeForm(clientId, redirect.Query().Get("code"))))
	}
	first := authorize()
	token, ok := findOAuthToken(t, first.AccessToken)
	if !ok || token.Name != "Claude Code" || *token.OAuthClientId != clientId {
		t.Fatalf("unexpected token %+v", token)
	}

	// Another machine with the same app keeps the first one logged in.
	second := authorize()
	if _, ok := findOAuthToken(t, first.AccessToken); !ok {
		t.Error("authorizing the app again logged out the first install")
	}
	if token, ok := findOAuthToken(t, second.AccessToken); !ok || token.Name != "Claude Code (2)" {
		t.Errorf("unexpected second token %+v", token)
	}

	// The client is public, a client sending a secret is someone else.
	form := url.Values{"grant_type": {grantTypeRefreshToken}, "client_id": {clientId}, "refresh_token": {second.RefreshToken}, "client_secret": {"x"}}
	if w := requestOAuthToken(router, form); w.Code != http.StatusUnauthorized || oauthErrorCode(w) != "invalid_client" {
		t.Errorf("expected invalid_client for a client secret, got %d: %s", w.Code, w.Body.String())
	}
	form.Del("client_secret")
	decodeOAuthToken(t, requestOAuthToken(router, form))

	// A redirect URI the document doesn't list is refused without a redirect.
	query.Set("redirect_uri", "https://evil.example/callback")
	if w := serveOAuth(router, httptest.NewRequest(http.MethodGet, "/api/v1/oauth/authorize?"+query.Encode(), nil)); w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "redirectUrl") {
		t.Errorf("expected 400 without a redirect, got %d: %s", w.Code, w.Body.String())
	}

	// Without the document, nothing is sent back to the client.
	query = authorizationQuery(server.URL + "/missing")
	w = serveOAuth(router, httptest.NewRequest(http.MethodGet, "/api/v1/oauth/authorize?"+query.Encode(), nil))
	if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "redirectUrl") {
		t.Errorf("expected 400 without a redirect, got %d: %s", w.Code, w.Body.String())
	}
}

// The OAuth client of the MCP SDK finds the support in the metadata of the
// hub and identifies itself with the URL of its document. Without a
// registration, golang.org/x/oauth2 first sends the token request with HTTP
// Basic credentials, which must not use up the code.
func TestOAuthFlow_MCPSDKClientWithMetadataDocument(t *testing.T) {
	user := setupOAuthTest(t)
	documents := newClientMetadataServer(t)
	const redirectURI = "http://localhost:45454/callback"
	clientId := documents.serve("/go-sdk", map[string]any{
		"client_name":   "Go SDK",
		"redirect_uris": []string{redirectURI},
		"grant_types":   []string{"authorization_code", "refresh_token"},
	})

	router := newOAuthRouter(&user, nil)
	hub := httptest.NewTLSServer(router)
	t.Cleanup(hub.Close)
	if err := InitOAuth("test-secret-that-is-long-enough-32chars", hub.URL); err != nil {
		t.Fatal(err)
	}
	router.GET("/.well-known/oauth-protected-resource/api/v1/mcp", OAuthProtectedResourceMetadataHandler())
	router.GET("/.well-known/oauth-authorization-server", OAuthAuthorizationServerMetadataHandler)

	// Trusts the test certificate. The custom dialer opts out of the SDK's
	// block of local addresses.
	transport := hub.Client().Transport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{}).DialContext
	httpClient := &http.Client{Transport: transport}

	browser := func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
		page, err := url.Parse(args.URL)
		if err != nil || page.Path != "/oauth/authorize" || page.Query().Get("client_id") != clientId {
			return nil, errors.New("unexpected authorization URL " + args.URL)
		}
		w, redirect := approveOAuth(t, router, page.Query(), true, "application:read")
		if w.Code != http.StatusOK {
			return nil, errors.New("consent failed: " + w.Body.String())
		}
		q := redirect.Query()
		return &mcpauth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}, nil
	}
	handler, err := mcpauth.NewAuthorizationCodeHandler(&mcpauth.AuthorizationCodeHandlerConfig{
		ClientIDMetadataDocumentConfig: &mcpauth.ClientIDMetadataDocumentConfig{URL: clientId},
		RedirectURL:                    redirectURI,
		AuthorizationCodeFetcher:       browser,
		Client:                         httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}

	// What the MCP endpoint answers without a token.
	req := httptest.NewRequest(http.MethodPost, hub.URL+"/api/v1/mcp", nil)
	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header:     http.Header{"Www-Authenticate": {MCPAuthChallenge()}},
		Body:       io.NopCloser(strings.NewReader("")),
	}
	if err := handler.Authorize(t.Context(), req, resp); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	source, err := handler.TokenSource(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	accessToken, err := source.Token()
	if err != nil {
		t.Fatal(err)
	}
	token, err := gorm.G[models.APIToken](db.DB).Where("token_hash = ?", apitoken.Hash(accessToken.AccessToken)).First(t.Context())
	if err != nil {
		t.Fatalf("the SDK's token is not a grant: %v", err)
	}
	if *token.OAuthClientId != clientId || token.Name != "Go SDK" {
		t.Errorf("unexpected grant %+v", token)
	}
}
