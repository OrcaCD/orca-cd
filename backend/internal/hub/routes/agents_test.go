package routes

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const testUpdatedName = "Updated Name"

func setupTestDBWithAgents(t *testing.T) {
	t.Helper()
	setupTestDB(t)
	if err := db.DB.AutoMigrate(&models.Agent{}); err != nil {
		t.Fatalf("failed to migrate Agent: %v", err)
	}
}

func createTestAgentRecord(t *testing.T, name, keyId string, status models.AgentStatus, lastSeen *time.Time) models.Agent {
	t.Helper()

	agent := models.Agent{
		Name:     crypto.EncryptedString(name),
		KeyId:    crypto.EncryptedString(keyId),
		Status:   status,
		LastSeen: lastSeen,
	}

	if err := db.DB.WithContext(t.Context()).Create(&agent).Error; err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	return agent
}

func TestListAgentsHandler_Empty(t *testing.T) {
	setupTestDBWithAgents(t)

	router := gin.New()
	router.GET("/api/v1/agents", ListAgentsHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body []agentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("expected 0 agents, got %d", len(body))
	}
}

func TestListAgentsHandler_ReturnsAgents(t *testing.T) {
	setupTestDBWithAgents(t)

	now := time.Now().UTC().Truncate(time.Second)
	createTestAgentRecord(t, "Offline Agent", "offline-key", models.AgentStatusOffline, nil)
	createTestAgentRecord(t, "Error Agent", "error-key", models.AgentStatusError, &now)

	router := gin.New()
	router.GET("/api/v1/agents", ListAgentsHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body []agentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(body))
	}

	byName := map[string]agentResponse{}
	for _, item := range body {
		byName[item.Name] = item
	}

	offline, ok := byName["Offline Agent"]
	if !ok {
		t.Fatal("expected Offline Agent in response")
	}
	if offline.Status != agentStatusOffline {
		t.Fatalf("expected offline status %q, got %q", agentStatusOffline, offline.Status)
	}
	if offline.LastSeen != nil {
		t.Fatal("expected lastSeen=nil for Offline Agent")
	}

	errorAgent, ok := byName["Error Agent"]
	if !ok {
		t.Fatal("expected Error Agent in response")
	}
	if errorAgent.Status != "error" {
		t.Fatalf("expected error status %q, got %q", "error", errorAgent.Status)
	}
	if errorAgent.LastSeen == nil {
		t.Fatal("expected lastSeen to be set for Error Agent")
	}
}

func TestGetAgentHandler_Success(t *testing.T) {
	setupTestDBWithAgents(t)

	now := time.Now().UTC().Truncate(time.Second)
	agent := createTestAgentRecord(t, "My Agent", "my-key", models.AgentStatusOnline, &now)

	router := gin.New()
	router.GET("/api/v1/agents/:id", GetAgentHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+agent.Id, nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body agentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body.Id != agent.Id {
		t.Fatalf("expected id %q, got %q", agent.Id, body.Id)
	}
	if body.Name != "My Agent" {
		t.Fatalf("expected name %q, got %q", "My Agent", body.Name)
	}
	if body.Status != "online" {
		t.Fatalf("expected status %q, got %q", "online", body.Status)
	}
	if body.LastSeen == nil {
		t.Fatal("expected lastSeen to be set")
	}
}

func TestGetAgentHandler_NotFound(t *testing.T) {
	setupTestDBWithAgents(t)

	router := gin.New()
	router.GET("/api/v1/agents/:id", GetAgentHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/missing", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgentHandler_Success(t *testing.T) {
	setupTestDBWithAgents(t)

	reqBody, _ := json.Marshal(map[string]any{"name": "New Agent"})

	router := gin.New()
	router.POST("/api/v1/agents", CreateAgentHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var body agentWithTokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Id == "" {
		t.Fatal("expected id to be set")
	}
	if body.Name != "New Agent" {
		t.Fatalf("expected name %q, got %q", "New Agent", body.Name)
	}
	if body.Status != agentStatusOffline {
		t.Fatalf("expected status %q, got %q", agentStatusOffline, body.Status)
	}
	if body.AuthToken == "" {
		t.Fatal("expected authToken to be set")
	}

	claims, err := auth.ValidateAgentToken(body.AuthToken)
	if err != nil {
		t.Fatalf("failed to validate returned auth token: %v", err)
	}
	if claims.Subject != body.Id {
		t.Fatalf("expected token subject %q, got %q", body.Id, claims.Subject)
	}
	if claims.KeyId == "" {
		t.Fatal("expected token KeyId to be set")
	}

	agent, err := gorm.G[models.Agent](db.DB).Where("id = ?", body.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load created agent: %v", err)
	}
	if agent.KeyId.String() == "" {
		t.Fatal("expected stored KeyId to be set")
	}
	if agent.KeyId.String() != claims.KeyId {
		t.Fatalf("expected stored KeyId %q to match token KeyId %q", agent.KeyId.String(), claims.KeyId)
	}
}

func TestCreateAgentHandler_InvalidRequest(t *testing.T) {
	setupTestDBWithAgents(t)

	reqBody, _ := json.Marshal(map[string]any{})

	router := gin.New()
	router.POST("/api/v1/agents", CreateAgentHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgentHandler_Success(t *testing.T) {
	setupTestDBWithAgents(t)

	agent := createTestAgentRecord(t, "Old Name", "existing-key", models.AgentStatusOffline, nil)

	reqBody, _ := json.Marshal(map[string]any{"name": testUpdatedName})

	router := gin.New()
	router.PUT("/api/v1/agents/:id", UpdateAgentHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/"+agent.Id, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated, err := gorm.G[models.Agent](db.DB).Where("id = ?", agent.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load updated agent: %v", err)
	}
	if updated.Name.String() != testUpdatedName {
		t.Fatalf("expected updated name %q, got %q", testUpdatedName, updated.Name.String())
	}
	if updated.KeyId.String() != "existing-key" {
		t.Fatalf("expected KeyId %q to stay unchanged, got %q", "existing-key", updated.KeyId.String())
	}
}

func TestUpdateAgentHandler_NotFound(t *testing.T) {
	setupTestDBWithAgents(t)

	reqBody, _ := json.Marshal(map[string]any{"name": testUpdatedName})

	router := gin.New()
	router.PUT("/api/v1/agents/:id", UpdateAgentHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/missing", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteAgentHandler_Success(t *testing.T) {
	setupTestDBWithAgents(t)

	agent := createTestAgentRecord(t, "Delete Me", "delete-key", models.AgentStatusOffline, nil)

	router := gin.New()
	router.DELETE("/api/v1/agents/:id", DeleteAgentHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/"+agent.Id, nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	_, err := gorm.G[models.Agent](db.DB).Where("id = ?", agent.Id).First(t.Context())
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected deleted agent to be missing, got err=%v", err)
	}
}

func TestDeleteAgentHandler_NotFound(t *testing.T) {
	setupTestDBWithAgents(t)

	router := gin.New()
	router.DELETE("/api/v1/agents/:id", DeleteAgentHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/missing", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
