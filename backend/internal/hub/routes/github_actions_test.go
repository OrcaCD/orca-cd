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

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/githubactions"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	jose "github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
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

	if claims.RunId == "" {
		claims.RunId = "default-run"
	}
	return s.signToken(t, claims)
}

func (s *ghActionsOIDCTestServer) signToken(t *testing.T, claims githubActionsClaims) string {
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
	if err := db.DB.AutoMigrate(&models.GitHubActionsTrigger{}, &models.GitHubActionsTriggerSkip{}); err != nil {
		t.Fatalf("migrate GitHub Actions tables: %v", err)
	}

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
		Name:                            "owner/testrepo",
		Url:                             "https://github.com/owner/testrepo",
		Provider:                        models.GitHub,
		AuthMethod:                      models.AuthMethodNone,
		SyncType:                        models.SyncTypeManual,
		SyncStatus:                      models.SyncStatusUnknown,
		GitHubActionsOIDCEnabled:        true,
		GitHubActionsOIDCAllowRepoSync:  true,
		GitHubActionsOIDCAllowImageSync: true,
		CreatedBy:                       "user-1",
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

	decodeTriggerResponse(t, w)
}

func TestGitHubActionsDeployHandler_SyncRejectedWhenRepositoryAlreadySyncing(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	nop := zerolog.Nop()
	applications.DefaultPoller = applications.NewPoller(&nop)
	t.Cleanup(func() { applications.DefaultPoller = nil })

	release, ok := applications.DefaultPoller.TryLockRepositorySync(repo.Id)
	if !ok {
		t.Fatal("failed to pre-lock repository sync slot")
	}
	defer release()

	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
		Sha:        "abc123",
	})

	c, w := makeGHActionsRequest(token, `{"syncRepo": true}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
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

func TestGitHubActionsDeployHandler_WorkflowAllowList_Match(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	if err := db.DB.Exec(`UPDATE repositories SET github_actions_oidc_allowed_workflows = ? WHERE id = ?`, `["deploy.yml"]`, repo.Id).Error; err != nil {
		t.Fatalf("failed to set allowed workflows: %v", err)
	}

	token := srv.makeToken(t, githubActionsClaims{
		Repository:  "owner/testrepo",
		Ref:         "refs/heads/main",
		RefType:     "branch",
		EventName:   "push",
		Sha:         "abc123",
		WorkflowRef: "owner/testrepo/.github/workflows/deploy.yml@refs/heads/main",
	})

	c, w := makeGHActionsRequest(token, `{"syncRepo": true}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_WorkflowAllowList_NoMatch(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	if err := db.DB.Exec(`UPDATE repositories SET github_actions_oidc_allowed_workflows = ? WHERE id = ?`, `["deploy.yml"]`, repo.Id).Error; err != nil {
		t.Fatalf("failed to set allowed workflows: %v", err)
	}

	token := srv.makeToken(t, githubActionsClaims{
		Repository:  "owner/testrepo",
		Ref:         "refs/heads/main",
		RefType:     "branch",
		EventName:   "push",
		Sha:         "abc123",
		WorkflowRef: "owner/testrepo/.github/workflows/other.yml@refs/heads/main",
	})

	c, w := makeGHActionsRequest(token, `{"syncRepo": true}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_WorkflowAllowList_MissingClaim(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	if err := db.DB.Exec(`UPDATE repositories SET github_actions_oidc_allowed_workflows = ? WHERE id = ?`, `["deploy.yml"]`, repo.Id).Error; err != nil {
		t.Fatalf("failed to set allowed workflows: %v", err)
	}

	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
		Sha:        "abc123",
		// WorkflowRef intentionally omitted
	})

	c, w := makeGHActionsRequest(token, `{"syncRepo": true}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_WorkflowAllowList_RepoPrefixMismatch(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	if err := db.DB.Exec(`UPDATE repositories SET github_actions_oidc_allowed_workflows = ? WHERE id = ?`, `["deploy.yml"]`, repo.Id).Error; err != nil {
		t.Fatalf("failed to set allowed workflows: %v", err)
	}

	// workflow_ref points at a reusable workflow in a different repository, but
	// happens to share a filename with the allow-list — must not match.
	token := srv.makeToken(t, githubActionsClaims{
		Repository:  "owner/testrepo",
		Ref:         "refs/heads/main",
		RefType:     "branch",
		EventName:   "push",
		Sha:         "abc123",
		WorkflowRef: "some-org/shared-workflows/.github/workflows/deploy.yml@refs/heads/main",
	})

	c, w := makeGHActionsRequest(token, `{"syncRepo": true}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_BranchAllowList_Match(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	if err := db.DB.Exec(`UPDATE repositories SET github_actions_oidc_allowed_branches = ? WHERE id = ?`, `["main"]`, repo.Id).Error; err != nil {
		t.Fatalf("failed to set allowed branches: %v", err)
	}

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
}

func TestGitHubActionsDeployHandler_BranchAllowList_NoMatch(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	if err := db.DB.Exec(`UPDATE repositories SET github_actions_oidc_allowed_branches = ? WHERE id = ?`, `["release"]`, repo.Id).Error; err != nil {
		t.Fatalf("failed to set allowed branches: %v", err)
	}

	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
		Sha:        "abc123",
	})

	c, w := makeGHActionsRequest(token, `{"syncRepo": true}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_BranchAllowList_TagResolvesToMixOfAllowedAndDisallowed(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id) // app is configured for branch "main"

	if err := db.DB.Exec(`UPDATE repositories SET github_actions_oidc_allowed_branches = ? WHERE id = ?`, `["main"]`, repo.Id).Error; err != nil {
		t.Fatalf("failed to set allowed branches: %v", err)
	}

	// The tag's commit resolves to two branches; only "main" is allowed.
	withGHProvider(t, ghActionsTagProvider{branches: []string{"main", "staging"}})

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

func TestGitHubActionsDeployHandler_ActionScope_RepoSyncDisallowed(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	if err := db.DB.Exec(`UPDATE repositories SET github_actions_oidc_allow_repo_sync = ? WHERE id = ?`, false, repo.Id).Error; err != nil {
		t.Fatalf("failed to disable repo sync: %v", err)
	}

	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
		Sha:        "abc123",
	})

	c, w := makeGHActionsRequest(token, `{"syncRepo": true}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_ActionScope_ImageSyncDisallowed(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	if err := db.DB.Exec(`UPDATE repositories SET github_actions_oidc_allow_image_sync = ? WHERE id = ?`, false, repo.Id).Error; err != nil {
		t.Fatalf("failed to disable image sync: %v", err)
	}

	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
		Sha:        "abc123",
	})

	c, w := makeGHActionsRequest(token, `{"pullImages": true}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_ActionScope_MixedRequestRejectedWhenOneDisallowed(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	if err := db.DB.Exec(`UPDATE repositories SET github_actions_oidc_allow_image_sync = ? WHERE id = ?`, false, repo.Id).Error; err != nil {
		t.Fatalf("failed to disable image sync: %v", err)
	}

	token := srv.makeToken(t, githubActionsClaims{
		Repository: "owner/testrepo",
		Ref:        "refs/heads/main",
		RefType:    "branch",
		EventName:  "push",
		Sha:        "abc123",
	})

	// syncRepo is allowed but pullImages is not — the whole request must be rejected,
	// not partially dispatched.
	c, w := makeGHActionsRequest(token, `{"syncRepo": true, "pullImages": true}`)
	GitHubActionsDeployHandler(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGitHubActionsDeployHandler_EmptyAllowListsUnrestricted(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)

	// No allow-lists configured on the seeded repo (default) — should behave exactly
	// like the pre-restriction handler.
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

func TestGitHubActionsTriggerStatusHandler(t *testing.T) {
	srv := setupGHActionsTest(t)
	repo := seedGHActionsRepo(t)
	seedGHActionsApp(t, repo.Id)
	claims := githubActionsClaims{
		Repository: "owner/testrepo", RunId: "123456",
		Ref: "refs/heads/main", RefType: "branch", EventName: "push", Sha: "abc123",
	}
	c, w := makeGHActionsRequest(srv.makeToken(t, claims), `{"syncRepo": true}`)
	GitHubActionsDeployHandler(c)
	trigger := decodeTriggerResponse(t, w)
	app := trigger.Applications[0]

	router := gin.New()
	router.GET("/api/v1/github-actions/triggers/:id", GitHubActionsTriggerStatusHandler)
	poll := func(t *testing.T, id, token string, wantCode int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/github-actions/triggers/"+id, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != wantCode {
			t.Fatalf("poll = %d, want %d: %s", recorder.Code, wantCode, recorder.Body.String())
		}
		return recorder
	}

	for _, tc := range []struct {
		name, repository, runID string
	}{
		{"different repository", "owner/other", claims.RunId},
		{"different workflow run", claims.Repository, "123457"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			poll(t, trigger.TriggerID, srv.makeToken(t, githubActionsClaims{Repository: tc.repository, RunId: tc.runID}), http.StatusNotFound)
		})
	}
	poll(t, trigger.TriggerID, srv.signToken(t, githubActionsClaims{Repository: claims.Repository}), http.StatusNotFound)
	poll(t, trigger.TriggerID, "invalid-token", http.StatusUnauthorized)
	poll(t, "unknown-trigger", srv.makeToken(t, claims), http.StatusNotFound)

	w = poll(t, trigger.TriggerID, srv.makeToken(t, claims), http.StatusOK)
	var result githubactions.Result
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode running result: %v", err)
	}
	if result.Status != "running" || len(result.Applications) != 1 || result.Applications[0].Status != "running" {
		t.Fatalf("expected to wait for the queued sync, got %+v", result)
	}

	if _, err := applicationevents.RecordCompleted(t.Context(), applicationevents.Params{
		ApplicationID: app.Id, TriggerID: &trigger.TriggerID, Type: models.ApplicationEventCommitSync,
		Source: models.ApplicationEventSourceGitHubActions, CommitHash: &claims.Sha,
	}, models.ApplicationEventNoChange, nil); err != nil {
		t.Fatalf("record no-change event: %v", err)
	}

	// Polling only needs the issuer, audience, repository and run claims.
	pollClaims := githubActionsClaims{Repository: "OWNER/testrepo", RunId: claims.RunId}
	w = poll(t, trigger.TriggerID, srv.makeToken(t, pollClaims), http.StatusOK)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("workflow results should not be cached")
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode completed result: %v", err)
	}
	if result.Status != "succeeded" || len(result.Applications) != 1 {
		t.Fatalf("expected a successful result, got %+v", result)
	}
	got := result.Applications[0]
	if got.Id != app.Id || got.Name != app.Name || got.Status != "succeeded" || len(got.Events) != 1 ||
		got.Events[0].Status != "no_change" || got.Events[0].Commit == nil || *got.Events[0].Commit != claims.Sha {
		t.Errorf("unexpected application result: %+v", got)
	}

	if _, err := gorm.G[models.GitHubActionsTrigger](db.DB).Where("id = ?", trigger.TriggerID).
		Update(t.Context(), "created_at", time.Now().Add(-25*time.Hour)); err != nil {
		t.Fatalf("expire trigger: %v", err)
	}
	poll(t, trigger.TriggerID, srv.makeToken(t, claims), http.StatusNotFound)

	if err := db.DB.Migrator().DropTable(&models.GitHubActionsTrigger{}); err != nil {
		t.Fatalf("drop trigger table: %v", err)
	}
	poll(t, trigger.TriggerID, srv.makeToken(t, claims), http.StatusInternalServerError)
	c, w = makeGHActionsRequest(srv.makeToken(t, claims), `{"syncRepo": true}`)
	GitHubActionsDeployHandler(c)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("deploy without a trigger table = %d, want 500", w.Code)
	}
}

type testGitHubActionsTriggerResponse struct {
	Message      string                      `json:"message"`
	TriggerID    string                      `json:"triggerId"`
	Applications []githubactions.Application `json:"applications"`
}

func decodeTriggerResponse(t *testing.T, w *httptest.ResponseRecorder) testGitHubActionsTriggerResponse {
	t.Helper()
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var body testGitHubActionsTriggerResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode trigger response: %v", err)
	}
	if body.Message != "deployment triggered" || body.TriggerID == "" || len(body.Applications) != 1 {
		t.Fatalf("unexpected trigger response: %+v", body)
	}
	return body
}

func TestGitHubActionsImagePullDispatchFailure(t *testing.T) {
	for _, tc := range []struct {
		name, request string
		wait          bool
	}{
		{"fire and forget", `{"pullImages":true}`, false},
		{"wait for reports", `{"pullImages":true,"waitForResult":true}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := setupGHActionsTest(t)
			repo := seedGHActionsRepo(t)
			seedGHActionsApp(t, repo.Id)
			token := srv.makeToken(t, githubActionsClaims{
				Repository: "owner/testrepo", RunId: "123456",
				Ref: "refs/heads/main", RefType: "branch", EventName: "push", Sha: "abc123",
			})
			c, w := makeGHActionsRequest(token, tc.request)
			GitHubActionsDeployHandler(c)
			if !tc.wait {
				if w.Code != http.StatusConflict {
					t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
				}
				return
			}
			trigger := decodeTriggerResponse(t, w)
			result, err := githubactions.GetResult(t.Context(), trigger.TriggerID, "owner/testrepo", "123456")
			if err != nil || result.Status != "failed" || len(result.Applications) != 1 {
				t.Fatalf("expected failed image pull, got %+v, %v", result, err)
			}
			app := result.Applications[0]
			if app.Status != "failed" || len(app.Events) != 1 ||
				app.Events[0].Type != models.ApplicationEventImageUpdate || app.Events[0].Error == nil {
				t.Errorf("expected a failed image update event with an error, got %+v", app)
			}
		})
	}
}
