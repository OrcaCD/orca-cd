package routes

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestOrca(t *testing.T) {
	w := serveRestAPI(t, RegisterOrca, http.MethodGet, "/api/v1/orca")

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
