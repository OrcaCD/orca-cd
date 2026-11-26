package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/gin-gonic/gin"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"gorm.io/gorm"
)

// An MCP client finds the authorization server from a 401 of the endpoint,
// registers, gets approved on the consent page, and calls the endpoint with the
// token it gets, the way Claude Code does it.
func TestOAuthFlow_MCPClient(t *testing.T) {
	const appURL = "https://hub.example.com"
	const secret = "test-secret-that-is-long-enough-32chars"
	testDB := setupMCPTestDB(t)
	if err := auth.Init(secret, appURL); err != nil {
		t.Fatal(err)
	}

	user := models.User{Name: "Alex", Email: "alex@example.com"}
	if err := gorm.G[models.User](testDB).Create(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	if err := rbac.AssignBuiltInRole(t.Context(), testDB, user.Id, models.RoleBuiltInAdmin); err != nil {
		t.Fatal(err)
	}
	session, err := auth.GenerateUserToken(&user)
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(middleware.ValidateOrigin(appURL))
	if err := RegisterRoutes(router, Config{AppURL: appURL, AppSecret: secret, DisableUI: true}); err != nil {
		t.Fatalf("Failed to register routes: %v", err)
	}
	serve := func(req *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	getJSON := func(path string, v any) {
		t.Helper()
		w := serve(httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: expected 200, got %d: %s", path, w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
			t.Fatalf("GET %s: invalid JSON: %v", path, err)
		}
	}

	// 1. The endpoint tells the client where its metadata is.
	w := listMCPTools(router, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	challenge := w.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `resource_metadata="`+appURL+`/.well-known/oauth-protected-resource/api/v1/mcp"`) {
		t.Fatalf("unexpected challenge %q", challenge)
	}
	if exposed := w.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(exposed, "WWW-Authenticate") {
		t.Errorf("clients in a browser can't read the challenge, exposed headers: %q", exposed)
	}

	// 2. The metadata leads to the authorization server.
	var resource struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	getJSON("/.well-known/oauth-protected-resource/api/v1/mcp", &resource)
	if resource.Resource != appURL+"/api/v1/mcp" || !slices.Equal(resource.AuthorizationServers, []string{appURL}) {
		t.Fatalf("unexpected resource metadata: %+v", resource)
	}
	var server struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		RegistrationEndpoint  string `json:"registration_endpoint"`
	}
	getJSON("/.well-known/oauth-authorization-server", &server)
	if server.Issuer != appURL || server.AuthorizationEndpoint != appURL+"/oauth/authorize" {
		t.Fatalf("unexpected server metadata: %+v", server)
	}
	path := func(endpoint string) string { return strings.TrimPrefix(endpoint, appURL) }

	// 3. The client registers, without a browser and so without an Origin.
	const redirectURI = "http://localhost:45454/callback"
	w = serve(httptest.NewRequest(http.MethodPost, path(server.RegistrationEndpoint),
		strings.NewReader(`{"client_name":"Claude Code","redirect_uris":["`+redirectURI+`"]}`)))
	if w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var client struct {
		ClientId string `json:"client_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &client)

	// 4. The user approves the client on the consent page.
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {client.ClientId},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"state":                 {"s"},
		"resource":              {resource.Resource},
	}
	consent := func(origin string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"query": query.Encode(), "approve": true, "scopes": []string{"application:read"}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/oauth/authorize", strings.NewReader(string(body)))
		req.Header.Set("Cookie", "orcacd_auth="+session)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		return serve(req)
	}
	// The session can't be used from another site.
	if w := consent("https://evil.example"); w.Code != http.StatusForbidden {
		t.Fatalf("consent from another origin: expected 403, got %d", w.Code)
	}
	w = consent(appURL)
	if w.Code != http.StatusOK {
		t.Fatalf("consent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var approved struct {
		RedirectUrl string `json:"redirectUrl"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &approved)
	redirect, _ := url.Parse(approved.RedirectUrl)

	// 5. The client exchanges the code, again without an Origin.
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {client.ClientId},
		"code":          {redirect.Query().Get("code")},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
		"resource":      {resource.Resource},
	}
	req := httptest.NewRequest(http.MethodPost, path(server.TokenEndpoint), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = serve(req)
	if w.Code != http.StatusOK {
		t.Fatalf("token: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &token)

	// 6. The token only gets the tools of the approved scope, although the user
	// is an admin.
	w = listMCPTools(router, token.AccessToken)
	if w.Code != http.StatusOK {
		t.Fatalf("tools/list: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if names, want := mcpToolNames(t, w), []string{"get_application", "list_applications"}; !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}

	// 7. The token only works on the MCP endpoint, so it can't read the REST API
	// or approve other clients.
	body, _ := json.Marshal(map[string]any{"query": query.Encode(), "approve": true, "scopes": []string{"application:read"}})
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/oauth/authorize", strings.NewReader(string(body))),
	} {
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		if w := serve(req); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with the client's token: expected 401, got %d", req.Method, req.URL.Path, w.Code)
		}
	}

	// 8. Clients in a browser may call the client endpoints.
	req = httptest.NewRequest(http.MethodOptions, path(server.TokenEndpoint), nil)
	req.Header.Set("Origin", "https://inspector.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	if w := serve(req); w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("preflight of the token endpoint: got %d, %v", w.Code, w.Header())
	}
	req = httptest.NewRequest(http.MethodOptions, "/api/v1/mcp", nil)
	req.Header.Set("Origin", "https://inspector.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "authorization, content-type, mcp-protocol-version")
	if w := serve(req); w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "*" ||
		!strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Mcp-Protocol-Version") {
		t.Errorf("preflight of the MCP endpoint: got %d, %v", w.Code, w.Header())
	}
}

// The OAuth client of the MCP SDK discovers the server, registers, gets a
// token and calls the tools. It checks the metadata the way MCP clients do.
func TestOAuthFlow_MCPSDKClient(t *testing.T) {
	const secret = "test-secret-that-is-long-enough-32chars"
	testDB := setupMCPTestDB(t)

	srv := httptest.NewUnstartedServer(nil)
	srv.Start()
	t.Cleanup(srv.Close)
	// Passkeys need a domain, not an IP address.
	appURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	if err := auth.Init(secret, appURL); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(middleware.ValidateOrigin(appURL))
	if err := RegisterRoutes(router, Config{AppURL: appURL, AppSecret: secret, DisableUI: true}); err != nil {
		t.Fatalf("Failed to register routes: %v", err)
	}
	srv.Config.Handler = router

	user := models.User{Name: "Alex", Email: "alex@example.com"}
	if err := gorm.G[models.User](testDB).Create(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	if err := rbac.AssignBuiltInRole(t.Context(), testDB, user.Id, models.RoleBuiltInAdmin); err != nil {
		t.Fatal(err)
	}
	session, err := auth.GenerateUserToken(&user)
	if err != nil {
		t.Fatal(err)
	}

	// A custom dialer opts out of the SDK's block of local addresses.
	httpClient := &http.Client{Transport: &http.Transport{DialContext: (&net.Dialer{}).DialContext}}
	// The browser: the user opens the consent page and allows what the client
	// asks for.
	browser := func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
		page, err := url.Parse(args.URL)
		if err != nil || page.Path != "/oauth/authorize" {
			return nil, fmt.Errorf("unexpected authorization URL %q", args.URL)
		}
		var request struct {
			Scopes []struct {
				Scope string `json:"scope"`
			} `json:"scopes"`
		}
		get, _ := http.NewRequestWithContext(ctx, http.MethodGet, appURL+"/api/v1/oauth/authorize?"+page.RawQuery, nil)
		get.Header.Set("Cookie", "orcacd_auth="+session)
		if err := doJSON(httpClient, get, &request); err != nil {
			return nil, err
		}
		var scopes []string
		for _, s := range request.Scopes {
			scopes = append(scopes, s.Scope)
		}
		body, _ := json.Marshal(map[string]any{"query": page.RawQuery, "approve": true, "scopes": scopes})
		post, _ := http.NewRequestWithContext(ctx, http.MethodPost, appURL+"/api/v1/oauth/authorize", bytes.NewReader(body))
		post.Header.Set("Cookie", "orcacd_auth="+session)
		post.Header.Set("Origin", appURL)
		var answer struct {
			RedirectUrl string `json:"redirectUrl"`
		}
		if err := doJSON(httpClient, post, &answer); err != nil {
			return nil, err
		}
		redirect, err := url.Parse(answer.RedirectUrl)
		if err != nil {
			return nil, err
		}
		q := redirect.Query()
		return &mcpauth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}, nil
	}

	handler, err := mcpauth.NewAuthorizationCodeHandler(&mcpauth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &mcpauth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				ClientName:              "Go SDK",
				RedirectURIs:            []string{"http://localhost:45454/callback"},
				GrantTypes:              []string{"authorization_code", "refresh_token"},
				TokenEndpointAuthMethod: "none",
			},
		},
		AuthorizationCodeFetcher: browser,
		Client:                   httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	mcpSession, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:             appURL + "/api/v1/mcp",
		HTTPClient:           httpClient,
		OAuthHandler:         handler,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = mcpSession.Close() })

	tools, err := mcpSession.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	want := []string{"deploy_application", "get_application", "list_agents", "list_applications", "preview_deploy"}
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}

	tokens, err := gorm.G[models.APIToken](testDB).Where("user_id = ?", user.Id).Find(t.Context())
	if err != nil || len(tokens) != 1 || tokens[0].Name != "Go SDK" || !tokens[0].IsOAuth() {
		t.Errorf("expected one token for the client, got %+v (%v)", tokens, err)
	}
}

func doJSON(client *http.Client, req *http.Request, v any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: %d %s", req.Method, req.URL.Path, resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
