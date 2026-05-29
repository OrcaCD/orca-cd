package routes

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func ptr(s string) *string {
	return new(s)
}

func setupRoutesTestDB(t *testing.T) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test_routes.db")

	testDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: gormlogger.New(
			log.New(os.Stderr, "\n", log.LstdFlags),
			gormlogger.Config{LogLevel: gormlogger.Warn},
		),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	if err := testDB.AutoMigrate(&models.AuditLog{}, &models.User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	db.DB = testDB

	t.Cleanup(func() {
		sqlDB, _ := testDB.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		db.DB = nil
	})
}

func TestAdminListAuditLogsHandler_ReturnsLogsCorrectly(t *testing.T) {
	setupRoutesTestDB(t)

	gin.SetMode(gin.TestMode)

	mockLog := models.AuditLog{
		Base: models.Base{
			Id:        "log-123",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		EventType:  "user.login",
		UserId:     ptr("user-456"),
		TargetType: "system",
		TargetId:   ptr("target-789"),
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

	var response struct {
		Items   []map[string]any `json:"items"`
		HasMore bool             `json:"hasMore"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if len(response.Items) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(response.Items))
	}

	auditLogItem := response.Items[0]

	if auditLogItem["id"] != "log-123" {
		t.Errorf("expected id=log-123, got %v", auditLogItem["id"])
	}
	if auditLogItem["eventType"] != "user.login" {
		t.Errorf("expected eventType=user.login, got %v", auditLogItem["eventType"])
	}
	if auditLogItem["targetType"] != "system" {
		t.Errorf("expected targetType=system, got %v", auditLogItem["targetType"])
	}
	if auditLogItem["targetId"] != "target-789" {
		t.Errorf("expected targetId=target-789, got %v", auditLogItem["targetId"])
	}

	timeStr, ok := auditLogItem["createdAt"].(string)
	if !ok {
		t.Fatalf("expected createdAt to be a string, got %T", auditLogItem["createdAt"])
	}

	if _, err := time.Parse(time.RFC3339, timeStr); err != nil {
		t.Fatalf("expected time to be in RFC3339 format, got %s (error: %v)", timeStr, err)
	}
}

func TestAdminListAuditLogsHandler_LimitAndHasMore(t *testing.T) {
	setupRoutesTestDB(t)
	gin.SetMode(gin.TestMode)

	for i := 0; i < 2; i++ {
		mockLog := models.AuditLog{
			Base: models.Base{
				Id:        fmt.Sprintf("log-%d", i),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			EventType:  "user.login",
			TargetType: "system",
		}

		if err := db.DB.Create(&mockLog).Error; err != nil {
			t.Fatalf("failed to create log: %v", err)
		}
	}

	router := gin.New()
	router.GET("/api/v1/admin/audit-logs", AdminListAuditLogsHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs?limit=1", nil)

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var response struct {
		Items   []map[string]any `json:"items"`
		HasMore bool             `json:"hasMore"`
	}

	_ = json.Unmarshal(w.Body.Bytes(), &response)

	if len(response.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(response.Items))
	}

	if response.HasMore != true {
		t.Fatalf("expected hasMore=true")
	}
}

func TestAdminListAuditLogsHandler_Cursor(t *testing.T) {
    setupRoutesTestDB(t)
    gin.SetMode(gin.TestMode)

    oldDB := db.DB
    db.DB = db.DB.Debug() 
    defer func() { db.DB = oldDB }()

    base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).Truncate(time.Second)

    _ = db.DB.Exec("DELETE FROM audit_logs")

    old := models.AuditLog{
        Base: models.Base{
            Id:        "old",
            CreatedAt: base.Add(-10 * time.Minute).Truncate(time.Second),
        },
        EventType:  "user.login",
        TargetType: "system",
    }

    newLog := models.AuditLog{
        Base: models.Base{
            Id:        "new",
            CreatedAt: base.Add(10 * time.Minute).Truncate(time.Second),
        },
        EventType:  "user.login",
        TargetType: "system",
    }

    if err := db.DB.Save(&old).Error; err != nil {
        t.Fatalf("insert old failed: %v", err)
    }
    if err := db.DB.Save(&newLog).Error; err != nil {
        t.Fatalf("insert new failed: %v", err)
    }

    var count int64
    db.DB.Model(&models.AuditLog{}).Count(&count)
    t.Logf("Datensätze in der DB vor dem Request: %d", count)

    router := gin.New()
    router.GET("/api/v1/admin/audit-logs", AdminListAuditLogsHandler)

    cursor := base.Format(time.RFC3339)

    req := httptest.NewRequest(
        http.MethodGet,
        "/api/v1/admin/audit-logs?cursor="+cursor,
        nil,
    )

    w := httptest.NewRecorder()
    router.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d. Body: %s", w.Code, w.Body.String())
    }

    var response struct {
        Items []map[string]any `json:"items"`
    }

    if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
        t.Fatalf("invalid json: %v", err)
    }

    if len(response.Items) == 0 {
        t.Fatalf("expected at least 1 item, aber Response-Body war: %s", w.Body.String())
    }

    if response.Items[0]["id"] != "old" {
        t.Fatalf("expected old log, got %v", response.Items[0]["id"])
    }
}

func TestAdminListAuditLogsHandler_InvalidLimit(t *testing.T) {
	setupRoutesTestDB(t)
	gin.SetMode(gin.TestMode)

	mockLog := models.AuditLog{
		Base: models.Base{
			Id: "log-1",
		},
		EventType:  "user.login",
		TargetType: "system",
	}

	_ = db.DB.Create(&mockLog).Error

	router := gin.New()
	router.GET("/api/v1/admin/audit-logs", AdminListAuditLogsHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit-logs?limit=abc", nil)

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
