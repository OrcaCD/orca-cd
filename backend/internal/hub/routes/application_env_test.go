package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/envrules"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type envTestAgent struct {
	mu         sync.Mutex
	listReqs   []*messages.EnvListRequest
	updateReqs []*messages.EnvUpdateRequest
}

func (a *envTestAgent) updates() []*messages.EnvUpdateRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*messages.EnvUpdateRequest(nil), a.updateReqs...)
}

func withEnvAgent(t *testing.T, agentID string, list *messages.EnvListResult, update *messages.EnvUpdateResult) *envTestAgent {
	t.Helper()
	nop := zerolog.Nop()
	hub := websocket.NewHub(&nop)
	websocket.DefaultHub = hub
	t.Cleanup(func() { websocket.DefaultHub = nil })

	client, err := hub.Register(agentID, nil)
	if err != nil {
		t.Fatalf("failed to register fake agent: %v", err)
	}

	agent := &envTestAgent{}
	go func() {
		for msg := range client.Send {
			if req := msg.GetEnvListRequest(); req != nil {
				agent.mu.Lock()
				agent.listReqs = append(agent.listReqs, req)
				agent.mu.Unlock()
				result := list
				result.RequestId, result.ApplicationId = req.RequestId, req.ApplicationId
				hub.ResolveEnvListResult(agentID, result)
			}
			if req := msg.GetEnvUpdateRequest(); req != nil {
				agent.mu.Lock()
				agent.updateReqs = append(agent.updateReqs, req)
				agent.mu.Unlock()
				result := update
				result.RequestId, result.ApplicationId = req.RequestId, req.ApplicationId
				hub.ResolveEnvUpdateResult(agentID, result)
			}
		}
	}()
	t.Cleanup(client.Close)
	return agent
}

func setupEnvTest(t *testing.T, agentVersion string) models.Application {
	t.Helper()
	setupTestDBWithApplications(t)
	if err := db.DB.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("migrate audit logs: %v", err)
	}

	repo := seedTestRepository(t, "https://github.com/owner/repo-env")
	agent := seedTestAgent(t, "agent-env")
	if agentVersion != "" {
		if _, err := gorm.G[models.Agent](db.DB).Where("id = ?", agent.Id).Update(context.Background(), "version", agentVersion); err != nil {
			t.Fatalf("set agent version: %v", err)
		}
	}
	return seedTestApplication(t, repo.Id, agent.Id, "Env App")
}

func envRequest(t *testing.T, handler gin.HandlerFunc, method, appID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(method, "/api/v1/applications/"+appID+"/env", reader)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: appID}}
	handler(c)
	return w
}

func validEnvUpdateBody() updateEnvRequest {
	return updateEnvRequest{Files: []envFileChangesRequest{{
		Path:             ".env",
		ExpectedRevision: "rev-1",
		Changes: []envChangeRequest{
			{Key: "LOG_LEVEL", Value: "debug"},
			{Key: "DB_PASSWORD", Value: "hunter2", IsSecret: true},
			{Key: "OLD", Delete: true},
		},
	}}}
}

func countEnvAuditLogs(t *testing.T, appID string) int64 {
	t.Helper()
	count, err := gorm.G[models.AuditLog](db.DB).
		Where("event_type = ? AND target_type = ? AND target_id = ?", "updated-environment", "application", appID).
		Count(context.Background(), "*")
	if err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	return count
}

func TestListApplicationEnvHandler_Success(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	withEnvAgent(t, app.AgentId, &messages.EnvListResult{
		Success: true,
		EnvFiles: []*messages.EnvFile{{
			Path:     ".env",
			Services: []string{"web"},
			Revision: "rev-1",
			Entries: []*messages.EnvEntry{
				{Key: "LOG_LEVEL", Value: "debug"},
				{Key: "DB_PASSWORD", IsSecret: true, IsSet: true, Value: "hunter2"},
				{Key: "EMPTY_SECRET", IsSecret: true, IsSet: false},
			},
		}},
	}, nil)

	w := envRequest(t, ListApplicationEnvHandler, http.MethodGet, app.Id, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "hunter2") {
		t.Fatalf("secret value leaked in response: %s", w.Body.String())
	}

	var body envListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Files) != 1 || body.Files[0].Path != ".env" || body.Files[0].Revision != "rev-1" || len(body.Files[0].Entries) != 3 {
		t.Fatalf("unexpected response: %+v", body)
	}
	entries := body.Files[0].Entries
	if entries[0].Value == nil || *entries[0].Value != "debug" || entries[0].IsSecret {
		t.Errorf("unexpected variable entry: %+v", entries[0])
	}
	if entries[1].Value != nil || !entries[1].IsSecret || !entries[1].IsSet {
		t.Errorf("unexpected secret entry: %+v", entries[1])
	}
	if entries[2].IsSet {
		t.Errorf("unset secret reported as set: %+v", entries[2])
	}
}

func TestListApplicationEnvHandler_NoFilesIsEmptyArray(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	withEnvAgent(t, app.AgentId, &messages.EnvListResult{Success: true}, nil)

	w := envRequest(t, ListApplicationEnvHandler, http.MethodGet, app.Id, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"files":[]`) {
		t.Fatalf("expected empty files array, got %s", w.Body.String())
	}
}

func TestListApplicationEnvHandler_NotFound(t *testing.T) {
	setupTestDBWithApplications(t)
	withOfflineHub(t)

	w := envRequest(t, ListApplicationEnvHandler, http.MethodGet, "missing", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListApplicationEnvHandler_AgentOffline(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	withOfflineHub(t)

	w := envRequest(t, ListApplicationEnvHandler, http.MethodGet, app.Id, nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListApplicationEnvHandler_OutdatedAgent(t *testing.T) {
	for _, agentVersion := range []string{"", "v0.5.0"} {
		app := setupEnvTest(t, agentVersion)
		agent := withEnvAgent(t, app.AgentId, &messages.EnvListResult{Success: true}, nil)

		w := envRequest(t, ListApplicationEnvHandler, http.MethodGet, app.Id, nil)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("version %q: expected 422, got %d: %s", agentVersion, w.Code, w.Body.String())
		}
		if len(agent.listReqs) != 0 {
			t.Fatalf("version %q: request must not reach an outdated agent", agentVersion)
		}
	}
}

func TestListApplicationEnvHandler_AgentError(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	withEnvAgent(t, app.AgentId, &messages.EnvListResult{ErrorMessage: "compose file not found"}, nil)

	w := envRequest(t, ListApplicationEnvHandler, http.MethodGet, app.Id, nil)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "compose file not found") {
		t.Fatalf("expected agent error in body, got %s", w.Body.String())
	}
}

func TestListApplicationEnvHandler_Timeout(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	nop := zerolog.Nop()
	hub := websocket.NewHub(&nop)
	websocket.DefaultHub = hub
	t.Cleanup(func() { websocket.DefaultHub = nil })
	if _, err := hub.Register(app.AgentId, nil); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}
	ListApplicationEnvHandler(c)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateApplicationEnvHandler_Success(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	agent := withEnvAgent(t, app.AgentId, nil, &messages.EnvUpdateResult{Success: true})

	w := envRequest(t, UpdateApplicationEnvHandler, http.MethodPut, app.Id, validEnvUpdateBody())
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "hunter2") {
		t.Fatalf("secret value leaked in response: %s", w.Body.String())
	}

	reqs := agent.updates()
	if len(reqs) != 1 {
		t.Fatalf("expected one update request, got %d", len(reqs))
	}
	file := reqs[0].Files[0]
	if file.Path != ".env" || file.ExpectedRevision != "rev-1" || len(file.Changes) != 3 {
		t.Fatalf("unexpected forwarded file: %+v", file)
	}
	if !file.Changes[1].IsSecret || file.Changes[1].Value != "hunter2" || !file.Changes[2].Delete {
		t.Fatalf("unexpected forwarded changes: %+v", file.Changes)
	}
	if got := countEnvAuditLogs(t, app.Id); got != 1 {
		t.Fatalf("expected one audit entry, got %d", got)
	}
}

func TestUpdateApplicationEnvHandler_DeleteDropsValue(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	agent := withEnvAgent(t, app.AgentId, nil, &messages.EnvUpdateResult{Success: true})

	body := updateEnvRequest{Files: []envFileChangesRequest{{
		Path:             ".env",
		ExpectedRevision: "rev-1",
		Changes:          []envChangeRequest{{Key: "A", Delete: true, Value: "leftover"}},
	}}}
	if w := envRequest(t, UpdateApplicationEnvHandler, http.MethodPut, app.Id, body); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := agent.updates()[0].Files[0].Changes[0].Value; got != "" {
		t.Fatalf("expected value of deleted key to be dropped, got %q", got)
	}
}

func TestUpdateApplicationEnvHandler_Conflict(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	withEnvAgent(t, app.AgentId, nil, &messages.EnvUpdateResult{Conflict: true, ErrorMessage: "revision mismatch"})

	w := envRequest(t, UpdateApplicationEnvHandler, http.MethodPut, app.Id, validEnvUpdateBody())
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"conflict":true`) {
		t.Fatalf("expected conflict flag, got %s", w.Body.String())
	}
	if got := countEnvAuditLogs(t, app.Id); got != 0 {
		t.Fatalf("conflict must not be audited, got %d entries", got)
	}
}

func TestUpdateApplicationEnvHandler_AgentFailure(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	withEnvAgent(t, app.AgentId, nil, &messages.EnvUpdateResult{ErrorMessage: "env file .env is not declared"})

	w := envRequest(t, UpdateApplicationEnvHandler, http.MethodPut, app.Id, validEnvUpdateBody())
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not declared") {
		t.Fatalf("expected agent error in body, got %s", w.Body.String())
	}
	if got := countEnvAuditLogs(t, app.Id); got != 0 {
		t.Fatalf("nothing was saved, got %d audit entries", got)
	}
}

func TestUpdateApplicationEnvHandler_SavedButRedeployFailed(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	withEnvAgent(t, app.AgentId, nil, &messages.EnvUpdateResult{ErrorMessage: envrules.SavedRedeployFailedPrefix + ": boom"})

	w := envRequest(t, UpdateApplicationEnvHandler, http.MethodPut, app.Id, validEnvUpdateBody())
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "boom") {
		t.Fatalf("expected agent error in body, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"savedButRedeployFailed":true`) {
		t.Fatalf("expected savedButRedeployFailed flag, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"error":"The changes were saved`) {
		t.Fatalf("expected capitalized message, got %s", w.Body.String())
	}
	if got := countEnvAuditLogs(t, app.Id); got != 1 {
		t.Fatalf("saved values must be audited, got %d entries", got)
	}
}

func TestUpdateApplicationEnvHandler_AgentOffline(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	withOfflineHub(t)

	w := envRequest(t, UpdateApplicationEnvHandler, http.MethodPut, app.Id, validEnvUpdateBody())
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateApplicationEnvHandler_OutdatedAgent(t *testing.T) {
	app := setupEnvTest(t, "v0.5.0")
	agent := withEnvAgent(t, app.AgentId, nil, &messages.EnvUpdateResult{Success: true})

	w := envRequest(t, UpdateApplicationEnvHandler, http.MethodPut, app.Id, validEnvUpdateBody())
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if len(agent.updates()) != 0 {
		t.Fatal("request must not reach an outdated agent")
	}
}

func TestUpdateApplicationEnvHandler_NotFound(t *testing.T) {
	setupTestDBWithApplications(t)
	withOfflineHub(t)

	w := envRequest(t, UpdateApplicationEnvHandler, http.MethodPut, "missing", validEnvUpdateBody())
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateApplicationEnvHandler_Validation(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	agent := withEnvAgent(t, app.AgentId, nil, &messages.EnvUpdateResult{Success: true})

	file := func(changes ...envChangeRequest) envFileChangesRequest {
		return envFileChangesRequest{Path: ".env", ExpectedRevision: "rev", Changes: changes}
	}
	manyChanges := make([]envChangeRequest, envrules.MaxKeysPerFile+1)
	for i := range manyChanges {
		manyChanges[i] = envChangeRequest{Key: "K" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + strings.Repeat("_", i/26), Value: "v"}
	}

	tests := []struct {
		name  string
		files []envFileChangesRequest
	}{
		{"no files", nil},
		{"empty path", []envFileChangesRequest{{Path: "", ExpectedRevision: "rev", Changes: []envChangeRequest{{Key: "A", Value: "1"}}}}},
		{"missing revision", []envFileChangesRequest{{Path: ".env", Changes: []envChangeRequest{{Key: "A", Value: "1"}}}}},
		{"no changes in file", []envFileChangesRequest{file()}},
		{"duplicate path", []envFileChangesRequest{file(envChangeRequest{Key: "A", Value: "1"}), file(envChangeRequest{Key: "B", Value: "1"})}},
		{"duplicate key", []envFileChangesRequest{file(envChangeRequest{Key: "A", Value: "1"}, envChangeRequest{Key: "A", Value: "2"})}},
		{"empty key", []envFileChangesRequest{file(envChangeRequest{Key: "", Value: "1"})}},
		{"invalid key", []envFileChangesRequest{file(envChangeRequest{Key: "A=B", Value: "1"})}},
		{"value too large", []envFileChangesRequest{file(envChangeRequest{Key: "A", Value: strings.Repeat("x", envrules.MaxValueBytes+1)})}},
		{"control character in value", []envFileChangesRequest{file(envChangeRequest{Key: "A", Value: "a\x00b"})}},
		{"too many changes", []envFileChangesRequest{file(manyChanges...)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := envRequest(t, UpdateApplicationEnvHandler, http.MethodPut, app.Id, updateEnvRequest{Files: tt.files})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}

	if len(agent.updates()) != 0 {
		t.Fatal("invalid requests must not reach the agent")
	}
}

func TestValidateEnvUpdate_InvalidUTF8(t *testing.T) {
	tests := map[string]envFileChangesRequest{
		"path":  {Path: "a\xff.env", ExpectedRevision: "rev", Changes: []envChangeRequest{{Key: "A", Value: "1"}}},
		"key":   {Path: ".env", ExpectedRevision: "rev", Changes: []envChangeRequest{{Key: "A\xff", Value: "1"}}},
		"value": {Path: ".env", ExpectedRevision: "rev", Changes: []envChangeRequest{{Key: "A", Value: "a\xffb"}}},
	}
	for name, file := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := validateEnvUpdate([]envFileChangesRequest{file}); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestUpdateApplicationEnvHandler_ValidationErrorsContainNoValues(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	withEnvAgent(t, app.AgentId, nil, &messages.EnvUpdateResult{Success: true})

	body := updateEnvRequest{Files: []envFileChangesRequest{{
		Path:             ".env",
		ExpectedRevision: "rev",
		Changes:          []envChangeRequest{{Key: "A", Value: "sup3r-secret\x00"}},
	}}}
	w := envRequest(t, UpdateApplicationEnvHandler, http.MethodPut, app.Id, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "sup3r-secret") {
		t.Fatalf("validation error leaked the value: %s", w.Body.String())
	}
}

func TestUpdateApplicationEnvHandler_BlockedInDemoMode(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	agent := withEnvAgent(t, app.AgentId, nil, &messages.EnvUpdateResult{Success: true})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.DemoBlocking())
	router.PUT("/api/v1/applications/:id/env", UpdateApplicationEnvHandler)

	raw, _ := json.Marshal(validEnvUpdateBody())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+app.Id+"/env", bytes.NewReader(raw)))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if len(agent.updates()) != 0 {
		t.Fatal("demo mode must not forward updates")
	}
}

func TestListApplicationEnvHandler_DemoModeServesSeededEntries(t *testing.T) {
	app := setupEnvTest(t, "")
	DemoMode = true
	t.Cleanup(func() { DemoMode = false })

	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).Update(context.Background(), "id", demoEnvWebAppID); err != nil {
		t.Fatalf("rename application: %v", err)
	}

	// No hub and no connected agent, like in a real demo
	w := envRequest(t, ListApplicationEnvHandler, http.MethodGet, demoEnvWebAppID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body envListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Files) == 0 {
		t.Fatal("expected demo env files")
	}

	var secrets, variables int
	for _, file := range body.Files {
		if file.Revision == "" {
			t.Errorf("file %q has no revision", file.Path)
		}
		for _, entry := range file.Entries {
			if entry.IsSecret {
				secrets++
				if entry.Value != nil || !entry.IsSet {
					t.Errorf("secret %q must be set and must not expose a value", entry.Key)
				}
			} else {
				variables++
				if entry.Value == nil {
					t.Errorf("variable %q has no value", entry.Key)
				}
			}
		}
	}
	if secrets == 0 || variables == 0 {
		t.Fatalf("expected secrets and variables, got %d and %d", secrets, variables)
	}
}

func TestDemoEnvFiles_MonitoringUsesOneFilePerService(t *testing.T) {
	files := demoEnvFiles(demoEnvMonitoringAppID)

	got := map[string][]string{}
	for _, file := range files {
		got[file.Path] = file.Services
	}
	want := map[string][]string{"prometheus.env": {"prometheus"}, "grafana.env": {"grafana"}}
	if len(got) != len(want) {
		t.Fatalf("expected files %v, got %v", want, got)
	}
	for path, services := range want {
		if len(got[path]) != 1 || got[path][0] != services[0] {
			t.Errorf("file %q: expected services %v, got %v", path, services, got[path])
		}
	}
}

func TestListApplicationEnvHandler_DemoModeWithoutSeededEntries(t *testing.T) {
	app := setupEnvTest(t, "")
	DemoMode = true
	t.Cleanup(func() { DemoMode = false })

	w := envRequest(t, ListApplicationEnvHandler, http.MethodGet, app.Id, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body envListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Files == nil || len(body.Files) != 0 {
		t.Fatalf("expected an empty file list, got %+v", body.Files)
	}
}

func TestListApplicationEnvHandler_DemoModeUnknownApplication(t *testing.T) {
	setupEnvTest(t, "")
	DemoMode = true
	t.Cleanup(func() { DemoMode = false })

	w := envRequest(t, ListApplicationEnvHandler, http.MethodGet, "missing", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (r *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	r.deadline = t
	return nil
}

func TestUpdateApplicationEnvHandler_ExtendsWriteDeadline(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	withEnvAgent(t, app.AgentId, nil, &messages.EnvUpdateResult{Success: true})

	raw, _ := json.Marshal(validEnvUpdateBody())
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(w)
	auth.SetClaims(c, &auth.UserClaims{Subject: "user-1"})
	auth.SetUser(c, &models.User{Id: "user-1", Name: "Test User", Email: "test@example.com", Role: models.UserRoleAdmin})
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+app.Id+"/env", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	start := time.Now()
	UpdateApplicationEnvHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	agentMax := envrules.LockWaitTimeout + envrules.WriteTimeout + envrules.RedeployTimeout
	if min := start.Add(agentMax); w.deadline.Before(min) {
		t.Fatalf("write deadline %v is earlier than the agent's maximum update time (%v)", w.deadline, min)
	}
}

func parseEnvRequest(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/env/parse", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	ParseEnvHandler(c)
	return w
}

func decodeParseEnvResponse(t *testing.T, w *httptest.ResponseRecorder) parseEnvResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp parseEnvResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func TestParseEnvHandler_ParsesComposeSyntax(t *testing.T) {
	content := strings.Join([]string{
		"# a comment",
		"PLAIN=value # trailing comment",
		"export EXPORTED=1",
		"COLON: yes",
		`DOUBLE="line1\nline2"`,
		`SINGLE='no \n escape'`,
		"MULTI=\"first",
		"second\"",
		"DUP=old",
		"DUP=new",
		"EMPTY=",
		"",
	}, "\n")

	resp := decodeParseEnvResponse(t, parseEnvRequest(t, gin.H{"content": content}))

	want := []parsedEnvEntry{
		{Key: "PLAIN", Value: "value", IsSecret: true},
		{Key: "EXPORTED", Value: "1", IsSecret: true},
		{Key: "COLON", Value: "yes", IsSecret: true},
		{Key: "DOUBLE", Value: "line1\nline2", IsSecret: true},
		{Key: "SINGLE", Value: `no \n escape`, IsSecret: true},
		{Key: "MULTI", Value: "first\nsecond", IsSecret: true},
		{Key: "DUP", Value: "new", IsSecret: true},
		{Key: "EMPTY", Value: "", IsSecret: true},
	}
	if len(resp.Entries) != len(want) {
		t.Fatalf("expected %d entries, got %d: %+v", len(want), len(resp.Entries), resp.Entries)
	}
	for i, entry := range want {
		if resp.Entries[i] != entry {
			t.Errorf("entry %d: expected %+v, got %+v", i, entry, resp.Entries[i])
		}
	}
	if resp.IgnoredLines != 0 {
		t.Errorf("expected no ignored lines, got %d", resp.IgnoredLines)
	}
}

func TestParseEnvHandler_PublicMarkerMakesVariable(t *testing.T) {
	content := "# orcacd:public\nLOG_LEVEL=debug\nTOKEN=abc\n"

	resp := decodeParseEnvResponse(t, parseEnvRequest(t, gin.H{"content": content}))

	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %+v", resp.Entries)
	}
	if resp.Entries[0].Key != "LOG_LEVEL" || resp.Entries[0].IsSecret {
		t.Errorf("marked key must be a variable, got %+v", resp.Entries[0])
	}
	if resp.Entries[1].Key != "TOKEN" || !resp.Entries[1].IsSecret {
		t.Errorf("unmarked key must be a secret, got %+v", resp.Entries[1])
	}
}

func TestParseEnvHandler_CountsIgnoredLines(t *testing.T) {
	content := "A=1\nnot an assignment\nBAD KEY=2\n\n# comment\n"

	resp := decodeParseEnvResponse(t, parseEnvRequest(t, gin.H{"content": content}))

	if len(resp.Entries) != 1 || resp.Entries[0].Key != "A" {
		t.Fatalf("expected only A, got %+v", resp.Entries)
	}
	if resp.IgnoredLines != 2 {
		t.Errorf("expected 2 ignored lines, got %d", resp.IgnoredLines)
	}
}

func TestParseEnvHandler_EmptyContentIsEmptyArray(t *testing.T) {
	w := parseEnvRequest(t, gin.H{"content": ""})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"entries":[]`) {
		t.Fatalf("expected an empty entries array, got %s", w.Body.String())
	}
}

func TestParseEnvHandler_InvalidBody(t *testing.T) {
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/env/parse", strings.NewReader("{nope"))
	c.Request.Header.Set("Content-Type", "application/json")
	ParseEnvHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestParseEnvHandler_RejectsOversizedContent(t *testing.T) {
	content := strings.Repeat("a", envrules.MaxFileBytes+1)

	w := parseEnvRequest(t, gin.H{"content": content})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestParseEnvHandler_RejectsOversizedBody(t *testing.T) {
	content := strings.Repeat("\x01", envrules.MaxFileBytes)
	raw, err := json.Marshal(gin.H{"content": content, "padding": strings.Repeat("a", 1<<20)})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/env/parse", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	ParseEnvHandler(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestParseEnvHandler_ErrorsContainNoValues(t *testing.T) {
	content := strings.Repeat("a", envrules.MaxFileBytes+1) + "=hunter2"

	w := parseEnvRequest(t, gin.H{"content": content})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "hunter2") {
		t.Fatalf("error leaked the content: %s", w.Body.String())
	}
}

func TestListApplicationEnvHandler_KeepsWriteDeadline(t *testing.T) {
	app := setupEnvTest(t, "v0.6.0")
	withEnvAgent(t, app.AgentId, &messages.EnvListResult{Success: true}, nil)

	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}
	ListApplicationEnvHandler(c)

	if !w.deadline.IsZero() {
		t.Fatalf("list must keep the server's write deadline, got %v", w.deadline)
	}
}
