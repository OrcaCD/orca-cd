package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/commitsigning"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func seedSigningTestRepo(t *testing.T, provider models.RepositoryProvider, url string) models.Repository {
	t.Helper()
	repo := models.Repository{
		Name:       "owner/signed-repo",
		Url:        url,
		Provider:   provider,
		AuthMethod: models.AuthMethodNone,
		SyncType:   models.SyncTypeManual,
		SyncStatus: models.SyncStatusUnknown,
		CreatedBy:  "user-1",
	}
	if err := db.DB.Select("*").Create(&repo).Error; err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}
	return repo
}

func updateRepositoryForTest(t *testing.T, id string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	reqBody, _ := json.Marshal(body)
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/repositories/"+id, bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: id}}
	UpdateRepositoryHandler(c)
	return w
}

func TestUpdateRepositoryHandler_CommitSigning(t *testing.T) {
	setupTestDBWithRepos(t)
	repo := seedSigningTestRepo(t, models.GitHub, "https://github.com/owner/signed-repo")
	key, err := os.ReadFile("../commitsigning/testdata/ssh_trusted.pub")
	if err != nil {
		t.Fatalf("read key: %v", err)
	}

	w := updateRepositoryForTest(t, repo.Id, map[string]any{
		"requireSignedCommits": true,
		"commitSigningKeys":    []string{"\n" + string(key) + "\n"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp repositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.CommitSigningSupported || !resp.RequireSignedCommits {
		t.Fatalf("expected signing to be supported and required, got %+v", resp)
	}
	if len(resp.CommitSigningKeys) != 1 {
		t.Fatalf("expected one signing key, got %+v", resp.CommitSigningKeys)
	}
	if got := resp.CommitSigningKeys[0]; got.Key != strings.TrimSpace(string(key)) || got.Identity != "trusted@orcacd.test" || got.Type != commitsigning.KeyTypeSSH {
		t.Fatalf("unexpected signing key: %+v", got)
	}

	stored, err := gorm.G[models.Repository](db.DB).Where("id = ?", repo.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load repository: %v", err)
	}
	if !stored.RequireSignedCommits || !slices.Equal(stored.CommitSigningKeys, []string{strings.TrimSpace(string(key))}) {
		t.Fatalf("signing settings not persisted: %+v", stored)
	}

	c, listW := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/repositories", nil)
	ListRepositoriesHandler(c)
	if listW.Code != http.StatusOK || strings.Contains(listW.Body.String(), "commitSigningKeys") {
		t.Fatalf("expected the list without signing keys, got %d: %s", listW.Code, listW.Body.String())
	}

	// Removing every key while signing is required is rejected.
	w = updateRepositoryForTest(t, repo.Id, map[string]any{"commitSigningKeys": []string{}})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "at least one trusted signing key") {
		t.Fatalf("expected 400 for missing keys, got %d: %s", w.Code, w.Body.String())
	}

	w = updateRepositoryForTest(t, repo.Id, map[string]any{"requireSignedCommits": false, "commitSigningKeys": []string{}})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 when disabling signing, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateRepositoryHandler_CommitSigningRejectsInvalidKeys(t *testing.T) {
	setupTestDBWithRepos(t)
	repo := seedSigningTestRepo(t, models.GitHub, "https://github.com/owner/signed-repo")

	w := updateRepositoryForTest(t, repo.Id, map[string]any{"commitSigningKeys": []string{"ssh-ed25519 not-a-key"}})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid commit signing keys") {
		t.Fatalf("expected 400 for invalid keys, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateRepositoryHandler_CommitSigningUnsupportedProvider(t *testing.T) {
	setupTestDBWithRepos(t)
	repo := seedSigningTestRepo(t, models.AzureDevOps, "https://dev.azure.com/org/project/_git/repo")

	w := updateRepositoryForTest(t, repo.Id, map[string]any{
		"requireSignedCommits": true,
		"commitSigningKeys":    []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILzLKC57O0T9rFN/f1l8+X4tAzHb1b8+LMaAJoDFAQ9/"},
	})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not supported for this provider") {
		t.Fatalf("expected 400 for unsupported provider, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateApplicationHandler_RejectsUnverifiedCommit(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-signed")
	repo.RequireSignedCommits = true
	repo.CommitSigningKeys = []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILzLKC57O0T9rFN/f1l8+X4tAzHb1b8+LMaAJoDFAQ9/"}
	if err := db.DB.Save(&repo).Error; err != nil {
		t.Fatalf("save repository: %v", err)
	}
	agent := seedTestAgent(t, "agent-signed")

	reqBody, _ := json.Marshal(map[string]any{
		"name":         "Signed Service",
		"repositoryId": repo.Id,
		"agentId":      agent.Id,
		"branch":       "main",
		"path":         "services/signed.yml",
	})
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications", bytes.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateApplicationHandler(c)

	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "signature verification") {
		t.Fatalf("expected 422 signature error, got %d: %s", w.Code, w.Body.String())
	}
	count, err := gorm.G[models.Application](db.DB).Where("repository_id = ?", repo.Id).Count(t.Context(), "*")
	if err != nil {
		t.Fatalf("count applications: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no application to be created, got %d", count)
	}
}
