package routes

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	jose "github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
	"github.com/rs/zerolog"
)

// ghActionsOIDCTestServer is a minimal OIDC provider for testing GitHub Actions token verification.
type ghActionsOIDCTestServer struct {
	*httptest.Server
	privateKey *rsa.PrivateKey
	keyID      string
}

func newGHActionsOIDCTestServer(t *testing.T) *ghActionsOIDCTestServer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	const kid = "gh-actions-test-key"

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)

	jwk := jose.JSONWebKey{
		Key:       &key.PublicKey,
		KeyID:     kid,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	jwksJSON, _ := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
	discoveryJSON, _ := json.Marshal(map[string]any{
		"issuer":   srv.URL,
		"jwks_uri": srv.URL + "/keys",
	})

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(discoveryJSON)
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON)
	})

	return &ghActionsOIDCTestServer{Server: srv, privateKey: key, keyID: kid}
}

// makeToken creates a signed JWT with the given GitHub Actions claims and the test server as issuer.
func (s *ghActionsOIDCTestServer) makeToken(t *testing.T, claims githubActionsClaims) string {
	t.Helper()

	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: s.privateKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", s.keyID),
	)
	if err != nil {
		t.Fatalf("create JWT signer: %v", err)
	}

	now := time.Now()
	std := josejwt.Claims{
		Issuer:    s.URL,
		Audience:  josejwt.Audience{githubActionsAppURL},
		Subject:   "repo:" + claims.Repository + ":ref:" + claims.Ref,
		Expiry:    josejwt.NewNumericDate(now.Add(time.Hour)),
		IssuedAt:  josejwt.NewNumericDate(now),
		NotBefore: josejwt.NewNumericDate(now),
	}

	raw, err := josejwt.Signed(sig).Claims(std).Claims(claims).Serialize()
	if err != nil {
		t.Fatalf("serialize JWT: %v", err)
	}
	return raw
}

// setupGHActionsTest initialises the test DB, a local OIDC server, the OIDC
// provider cache, and the application sync queue. All state is reset in t.Cleanup.
func setupGHActionsTest(t *testing.T) *ghActionsOIDCTestServer {
	t.Helper()
	setupTestDBWithRepos(t)

	srv := newGHActionsOIDCTestServer(t)

	p, err := gooidc.NewProvider(context.Background(), srv.URL)
	if err != nil {
		srv.Close()
		t.Fatalf("init test OIDC provider: %v", err)
	}

	ghProviderMu.Lock()
	ghProvider = p
	ghProviderMu.Unlock()

	githubActionsAppURL = "http://localhost:8080"

	nop := zerolog.Nop()
	applications.DefaultQueue = applications.NewQueue(&nop)

	t.Cleanup(func() {
		srv.Close()
		ghProviderMu.Lock()
		ghProvider = nil
		ghProviderMu.Unlock()
		githubActionsAppURL = ""
		applications.DefaultQueue = nil
	})

	return srv
}

func makeGHActionsRequest(token, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/github-actions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if token != "" {
		c.Request.Header.Set("Authorization", "Bearer "+token)
	}
	return c, w
}

func seedGHActionsRepo(t *testing.T) models.Repository {
	t.Helper()
	repo := models.Repository{
		Name:                     "owner/testrepo",
		Url:                      "https://github.com/owner/testrepo",
		Provider:                 models.GitHub,
		AuthMethod:               models.AuthMethodNone,
		SyncType:                 models.SyncTypeManual,
		SyncStatus:               models.SyncStatusUnknown,
		GitHubActionsOIDCEnabled: true,
		CreatedBy:                "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("seed GitHub Actions repo: %v", err)
	}
	return repo
}

func seedGHActionsApp(t *testing.T, repoID string) {
	t.Helper()
	agent := models.Agent{Name: crypto.EncryptedString("test-agent-gh")}
	if err := db.DB.Select("*").Create(&agent).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	app := models.Application{
		Name:         crypto.EncryptedString("test-app-gh"),
		RepositoryId: repoID,
		AgentId:      agent.Id,
		SyncStatus:   models.UnknownSync,
		HealthStatus: models.UnknownHealth,
		Branch:       "main",
		Path:         "docker-compose.yml",
		ComposeFile:  crypto.EncryptedString("version: '3'\n"),
	}
	if err := db.DB.Select("*").Create(&app).Error; err != nil {
		t.Fatalf("seed application: %v", err)
	}
}

// ghActionsTagProvider is a repositories.Provider that also implements
// CommitBranchResolver, used to test the tag ref path without real network calls.
type ghActionsTagProvider struct {
	branches []string
	err      error
}

func (ghActionsTagProvider) ParseURL(_ string) (string, string, error) {
	return "owner", "testrepo", nil
}
func (ghActionsTagProvider) SupportedAuthMethods() []models.RepositoryAuthMethod { return nil }
func (ghActionsTagProvider) TestConnection(_ context.Context, _ *models.Repository) error {
	return nil
}
func (ghActionsTagProvider) ListBranches(_ context.Context, _ *models.Repository) ([]string, error) {
	return nil, nil
}
func (ghActionsTagProvider) ListTree(_ context.Context, _ *models.Repository, _ string) ([]repositories.TreeEntry, error) {
	return nil, nil
}
func (ghActionsTagProvider) GetFileContent(_ context.Context, _ *models.Repository, _, _ string) (string, error) {
	return "", nil
}
func (ghActionsTagProvider) GetLatestCommit(_ context.Context, _ *models.Repository, _ string) (repositories.CommitInfo, error) {
	return repositories.CommitInfo{}, nil
}
func (p ghActionsTagProvider) GetBranchesForCommit(_ context.Context, _ *models.Repository, _ string) ([]string, error) {
	return p.branches, p.err
}

// ghActionsNoTagProvider is a repositories.Provider without CommitBranchResolver.
type ghActionsNoTagProvider struct{}

func (ghActionsNoTagProvider) ParseURL(_ string) (string, string, error) {
	return "owner", "testrepo", nil
}
func (ghActionsNoTagProvider) SupportedAuthMethods() []models.RepositoryAuthMethod { return nil }
func (ghActionsNoTagProvider) TestConnection(_ context.Context, _ *models.Repository) error {
	return nil
}
func (ghActionsNoTagProvider) ListBranches(_ context.Context, _ *models.Repository) ([]string, error) {
	return nil, nil
}
func (ghActionsNoTagProvider) ListTree(_ context.Context, _ *models.Repository, _ string) ([]repositories.TreeEntry, error) {
	return nil, nil
}
func (ghActionsNoTagProvider) GetFileContent(_ context.Context, _ *models.Repository, _, _ string) (string, error) {
	return "", nil
}
func (ghActionsNoTagProvider) GetLatestCommit(_ context.Context, _ *models.Repository, _ string) (repositories.CommitInfo, error) {
	return repositories.CommitInfo{}, nil
}

// withGHProvider temporarily registers p as the GitHub provider and restores the
// original on test cleanup.
func withGHProvider(t *testing.T, p repositories.Provider) {
	t.Helper()
	orig, err := repositories.Get(models.GitHub)
	if err != nil {
		t.Fatalf("get original GitHub provider: %v", err)
	}
	repositories.Register(models.GitHub, p)
	t.Cleanup(func() { repositories.Register(models.GitHub, orig) })
}

// --- Tests ---

func TestGitHubActionsDeployHandler_MissingToken(t *testing.T) {
	c, w := makeGHActionsRequest("", `{}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_EmptyBearerToken(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/github-actions", strings.NewReader(`{}`))
	c.Request.Header.Set("Authorization", "Bearer   ") // whitespace-only after prefix

	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_InvalidToken(t *testing.T) {
	setupGHActionsTest(t)

	c, w := makeGHActionsRequest("not-a-valid-jwt", `{}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_MissingClaims(t *testing.T) {
	srv := setupGHActionsTest(t)

	// Token with empty repository claim — must be rejected.
	token := srv.makeToken(t, githubActionsClaims{
		Repository: "",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
	})
	c, w := makeGHActionsRequest(token, `{}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_PREventBlocked(t *testing.T) {
	srv := setupGHActionsTest(t)
	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "pull_request",
	})

	c, w := makeGHActionsRequest(token, `{}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_PRTargetEventBlocked(t *testing.T) {
	srv := setupGHActionsTest(t)
	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "pull_request_target",
	})

	c, w := makeGHActionsRequest(token, `{}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_NoMatchingRepo(t *testing.T) {
	srv := setupGHActionsTest(t)
	// No repos seeded → nothing to match.
	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
	})

	c, w := makeGHActionsRequest(token, `{}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_RepoOIDCDisabled_NotMatched(t *testing.T) {
	srv := setupGHActionsTest(t)

	// Seed a repo for the same URL but without OIDC enabled.
	repo := models.Repository{
		Name:                     "owner/testrepo",
		Url:                      "https://github.com/owner/testrepo",
		Provider:                 models.GitHub,
		AuthMethod:               models.AuthMethodNone,
		SyncType:                 models.SyncTypeManual,
		SyncStatus:               models.SyncStatusUnknown,
		GitHubActionsOIDCEnabled: false,
		CreatedBy:                "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
	})
	c, w := makeGHActionsRequest(token, `{}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 when OIDC disabled, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_NoAppsForBranch(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)

	// No applications seeded for the branch → 202 "no applications found".
	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
		Sha:        "abc123",
	})
	_ = repo

	c, w := makeGHActionsRequest(token, `{}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["message"] != "no applications found for branch" {
		t.Errorf("unexpected message: %q", body["message"])
	}
}

func TestGitHubActionsDeployHandler_InvalidRequestBody(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
		Sha:        "abc123",
	})

	c, w := makeGHActionsRequest(token, "not-valid-json")
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_NeitherSyncNorPull(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
		Sha:        "abc123",
	})

	// Both syncRepo and pullImages are false by default.
	c, w := makeGHActionsRequest(token, `{"syncRepo": false, "pullImages": false}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_SyncTriggered(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
		Sha:        "abc123",
	})

	c, w := makeGHActionsRequest(token, `{"syncRepo": true}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["message"] != "deployment triggered" {
		t.Errorf("unexpected message: %q", body["message"])
	}
}

func TestGitHubActionsDeployHandler_UnsupportedRefType(t *testing.T) {
	srv := setupGHActionsTest(t)
	seedGHActionsRepo(t)

	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "commit", // not "branch" or "tag"
		EventName:  "push",
		Sha:        "abc123",
	})

	c, w := makeGHActionsRequest(token, `{}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_TagRef_NoCommitBranchResolver(t *testing.T) {
	srv := setupGHActionsTest(t)
	seedGHActionsRepo(t)
	withGHProvider(t, ghActionsNoTagProvider{})

	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/tags/v1.0.0",
		RefType:    "tag",
		EventName:  "push",
		Sha:        "abc123",
	})

	c, w := makeGHActionsRequest(token, `{}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_TagRef_ResolvesAndTriggers(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	// Register a mock provider whose GetBranchesForCommit returns "main".
	withGHProvider(t, ghActionsTagProvider{branches: []string{"main"}})

	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/tags/v1.0.0",
		RefType:    "tag",
		EventName:  "push",
		Sha:        "abc123",
	})

	c, w := makeGHActionsRequest(token, `{"syncRepo": true}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_CaseInsensitiveRepoMatch(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	// Repo URL is "owner/testrepo" but claim uses different casing.
	token := srv.makeToken(t, githubActionsClaims{
		Repository: "OWNER/TESTREPO",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
		Sha:        "abc123",
	})

	c, w := makeGHActionsRequest(token, `{"syncRepo": true}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected case-insensitive match to succeed (202), got %d: %s", w.Code, w.Body.String())
	}
}
