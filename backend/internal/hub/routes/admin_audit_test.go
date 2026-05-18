package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
)

func TestAdminListAuditLogsHandler_ReturnsLogsCorrectly(t *testing.T) {
	// Gin in den Testmodus versetzen
	gin.SetMode(gin.TestMode)

	// Falls du eine Test-DB aufräumen musst, kannst du das hier tun.
	// Wir leeren die Tabelle vor dem Test und optional danach.
	db.DB.Exec("DELETE FROM audit_logs")
	t.Cleanup(func() {
		db.DB.Exec("DELETE FROM audit_logs")
	})

	// Testdaten vorbereiten
	mockLog := models.AuditLog{
		Base: models.Base{
			Id:        "log-123",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		EventType:  "user.login",
		UserId:     new("user-456"),
		TargetType: "system",
		TargetId:   new("target-789"),
	}

	if err := db.DB.Create(&mockLog).Error; err != nil {
		t.Fatalf("failed to create mock audit log: %v", err)
	}

	router := gin.New()
	router.GET("/api/v1/admin/audit-logs", AdminListAuditLogsHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if len(response) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(response))
	}

	log := response[0]

	// Einzelne Felder prüfen
	if log["id"] != "log-123" {
		t.Fatalf("expected id=log-123, got %v", log["id"])
	}
	if log["eventType"] != "user.login" {
		t.Fatalf("expected eventType=user.login, got %v", log["eventType"])
	}
	if log["userId"] != "user-456" {
		t.Fatalf("expected userId=user-456, got %v", log["userId"])
	}
	if log["targetType"] != "system" {
		t.Fatalf("expected targetType=system, got %v", log["targetType"])
	}
	if log["targetId"] != "target-789" {
		t.Fatalf("expected targetId=target-789, got %v", log["targetId"])
	}

	// Zeitstempel auf valides RFC3339-Format prüfen
	timeStr, ok := log["time"].(string)
	if !ok {
		t.Fatalf("expected time to be a string, got %T", log["time"])
	}

	if _, err := time.Parse(time.RFC3339, timeStr); err != nil {
		t.Fatalf("expected time to be in RFC3339 format, got %s (error: %v)", timeStr, err)
	}
}
