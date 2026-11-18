package seclog

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func logEntry(t *testing.T, fn func(c *gin.Context)) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	Init(zerolog.New(&buf))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	c.Request.RemoteAddr = "192.0.2.10:4321"
	fn(c)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected a JSON log entry, got %q: %v", buf.String(), err)
	}
	return entry
}

func TestWarn_IncludesEventAndRequestFields(t *testing.T) {
	entry := logEntry(t, func(c *gin.Context) {
		Warn(c, LoginFailed, "reason", "wrong_password", "email", "a@example.com")
	})

	want := map[string]any{
		"level":     "warn",
		"category":  "security",
		"event":     LoginFailed,
		"client_ip": "192.0.2.10",
		"method":    http.MethodPost,
		"path":      "/api/v1/auth/login",
		"reason":    "wrong_password",
		"email":     "a@example.com",
	}
	for key, value := range want {
		if entry[key] != value {
			t.Errorf("%s: expected %v, got %v", key, value, entry[key])
		}
	}
}

func TestInfo_UsesInfoLevel(t *testing.T) {
	entry := logEntry(t, func(c *gin.Context) {
		Info(c, LoginSuccess)
	})

	if entry["level"] != "info" {
		t.Errorf("expected level info, got %v", entry["level"])
	}
	if entry["event"] != LoginSuccess {
		t.Errorf("expected event %s, got %v", LoginSuccess, entry["event"])
	}
}
