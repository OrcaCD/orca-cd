package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// withImageStatusAgent installs a hub with a fake connected agent that answers
// every ImageStatusRequest with result and forwards PullImagesRequests to pulls.
func withImageStatusAgent(t *testing.T, agentID string, result *messages.ImageStatusResult) (<-chan *messages.ImageStatusRequest, <-chan *messages.PullImagesRequest) {
	t.Helper()
	nop := zerolog.Nop()
	hub := websocket.NewHub(&nop)
	websocket.DefaultHub = hub
	t.Cleanup(func() { websocket.DefaultHub = nil })

	client, err := hub.Register(agentID, nil)
	if err != nil {
		t.Fatalf("failed to register fake agent: %v", err)
	}
	statusRequests := make(chan *messages.ImageStatusRequest, 1)
	pulls := make(chan *messages.PullImagesRequest, 1)
	go func() {
		for msg := range client.Send {
			if req := msg.GetPullImagesRequest(); req != nil {
				pulls <- req
				continue
			}
			req := msg.GetImageStatusRequest()
			if req == nil {
				continue
			}
			statusRequests <- req
			answer := &messages.ImageStatusResult{
				RequestId:     req.RequestId,
				ApplicationId: req.ApplicationId,
				Images:        result.GetImages(),
				ErrorMessage:  result.GetErrorMessage(),
			}
			hub.ResolveImageStatusResult(agentID, answer)
		}
	}()
	t.Cleanup(func() { hub.BeginDisconnect(client) })
	return statusRequests, pulls
}

func TestListApplicationImagesHandler_Success(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-images")
	agent := seedTestAgent(t, "agent-images")
	app := seedTestApplication(t, repo.Id, agent.Id, "Images App")

	requests, _ := withImageStatusAgent(t, agent.Id, &messages.ImageStatusResult{
		Images: []*messages.ServiceImageStatus{{
			Service:         "web",
			Image:           "nginx:latest",
			CurrentDigest:   "sha256:old",
			CurrentVersion:  "1.27.0",
			LatestDigest:    "sha256:new",
			UpdateAvailable: true,
		}},
	})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications/"+app.Id+"/images?latest=true", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	ListApplicationImagesHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	req := <-requests
	if req.ApplicationId != app.Id || req.ApplicationName != "Images App" || !req.CheckLatest {
		t.Fatalf("unexpected image status request: %+v", req)
	}

	var resp applicationImagesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := applicationImageResponse{
		Service:         "web",
		Image:           "nginx:latest",
		CurrentDigest:   "sha256:old",
		CurrentVersion:  "1.27.0",
		LatestDigest:    "sha256:new",
		UpdateAvailable: true,
	}
	if len(resp.Images) != 1 || resp.Images[0] != want {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestListApplicationImagesHandler_CurrentOnly(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-images-current")
	agent := seedTestAgent(t, "agent-images-current")
	app := seedTestApplication(t, repo.Id, agent.Id, "Images App")

	requests, _ := withImageStatusAgent(t, agent.Id, &messages.ImageStatusResult{})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications/"+app.Id+"/images", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	ListApplicationImagesHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if req := <-requests; req.CheckLatest {
		t.Fatal("expected the registry check to be skipped without ?latest=true")
	}
	if body := w.Body.String(); body != `{"images":[]}` {
		t.Fatalf("expected empty image list, got %s", body)
	}
}

func TestListApplicationImagesHandler_AgentError(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-images-error")
	agent := seedTestAgent(t, "agent-images-error")
	app := seedTestApplication(t, repo.Id, agent.Id, "Images App")

	withImageStatusAgent(t, agent.Id, &messages.ImageStatusResult{ErrorMessage: "compose file not found"})

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications/"+app.Id+"/images", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	ListApplicationImagesHandler(c)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListApplicationImagesHandler_AgentOffline(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-images-offline")
	agent := seedTestAgent(t, "agent-images-offline")
	app := seedTestApplication(t, repo.Id, agent.Id, "Images App")
	withOfflineHub(t)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications/"+app.Id+"/images", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	ListApplicationImagesHandler(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListApplicationImagesHandler_NotFound(t *testing.T) {
	setupTestDBWithApplications(t)
	withOfflineHub(t)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/applications/missing/images", nil)
	c.Params = gin.Params{{Key: "id", Value: "missing"}}

	ListApplicationImagesHandler(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPullApplicationImagesHandler_Success(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-pull")
	agent := seedTestAgent(t, "agent-pull")
	app := seedTestApplication(t, repo.Id, agent.Id, "Pull App")

	_, pulls := withImageStatusAgent(t, agent.Id, nil)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+app.Id+"/pull-images", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	PullApplicationImagesHandler(c)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	req := <-pulls
	if req.ApplicationId != app.Id {
		t.Fatalf("unexpected pull request: %+v", req)
	}

	event, err := gorm.G[models.ApplicationEvent](db.DB).Where("request_id = ?", req.RequestId).First(t.Context())
	if err != nil {
		t.Fatalf("load image update event: %v", err)
	}
	if event.Type != models.ApplicationEventImageUpdate ||
		event.Source != models.ApplicationEventSourceManual ||
		event.Status != models.ApplicationEventRunning ||
		event.ActorName == nil || *event.ActorName != "Test User" {
		t.Fatalf("unexpected image update event: %+v", event)
	}

	updated, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).First(t.Context())
	if err != nil {
		t.Fatalf("load application: %v", err)
	}
	if updated.SyncStatus != models.Syncing {
		t.Fatalf("expected application to be syncing, got %q", updated.SyncStatus)
	}
}

func TestPullApplicationImagesHandler_AgentOffline(t *testing.T) {
	setupTestDBWithApplications(t)
	repo := seedTestRepository(t, "https://github.com/owner/repo-pull-offline")
	agent := seedTestAgent(t, "agent-pull-offline")
	app := seedTestApplication(t, repo.Id, agent.Id, "Pull App")
	withOfflineHub(t)

	c, w := makeAuthContext(t, "user-1")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+app.Id+"/pull-images", nil)
	c.Params = gin.Params{{Key: "id", Value: app.Id}}

	PullApplicationImagesHandler(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}
