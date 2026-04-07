package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAdminSystemInfoHandler_ReturnsConfiguredValuesWithoutSecret(t *testing.T) {
	previous := adminSystemInfoConfig
	t.Cleanup(func() {
		adminSystemInfoConfig = previous
	})

	trustedProxies := []string{"10.0.0.1", "10.0.0.2"}
	SetAdminSystemInfoConfig(AdminSystemInfoConfig{
		Debug:            true,
		Host:             "127.0.0.1",
		Port:             "8080",
		LogLevel:         "debug",
		TrustedProxies:   trustedProxies,
		AppURL:           "https://example.com",
		DisableLocalAuth: true,
		Version:          "test",
	})

	trustedProxies[0] = "mutated"

	router := gin.New()
	router.GET("/api/v1/admin/system-info", AdminSystemInfoHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system-info", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body["debug"] != true {
		t.Fatalf("expected debug=true, got %v", body["debug"])
	}
	if body["host"] != "127.0.0.1" {
		t.Fatalf("expected host=127.0.0.1, got %v", body["host"])
	}
	if body["port"] != "8080" {
		t.Fatalf("expected port=8080, got %v", body["port"])
	}
	if body["log_level"] != "debug" {
		t.Fatalf("expected log_level=debug, got %v", body["log_level"])
	}
	if body["app_url"] != "https://example.com" {
		t.Fatalf("expected app_url=https://example.com, got %v", body["app_url"])
	}
	if body["disable_local_auth"] != true {
		t.Fatalf("expected disable_local_auth=true, got %v", body["disable_local_auth"])
	}
	if body["version"] != "test" {
		t.Fatalf("expected version=test, got %v", body["version"])
	}

	proxiesRaw, ok := body["trusted_proxies"].([]any)
	if !ok {
		t.Fatalf("expected trusted_proxies to be array, got %T", body["trusted_proxies"])
	}
	proxies := make([]string, 0, len(proxiesRaw))
	for _, v := range proxiesRaw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("expected proxy value to be string, got %T", v)
		}
		proxies = append(proxies, s)
	}
	if !reflect.DeepEqual(proxies, []string{"10.0.0.1", "10.0.0.2"}) {
		t.Fatalf("expected trusted proxies to be copied values, got %v", proxies)
	}

	if _, found := body["app_secret"]; found {
		t.Fatal("response must not include app_secret")
	}
}
