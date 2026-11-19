package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOrcaHandler(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/orca", nil)

	OrcaHandler(c)

	if w.Code != http.StatusTeapot {
		t.Errorf("expected 418, got %d", w.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if body["message"] != "I'm not a teapot, I'm an orca 🐋" {
		t.Errorf("unexpected message %q", body["message"])
	}
}
