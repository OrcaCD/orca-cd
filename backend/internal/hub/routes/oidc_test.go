package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOIDCCallbackHandler_ErrorParam(t *testing.T) {
	setupTestDB(t)

	router := gin.New()
	router.GET("/api/v1/auth/oidc/:id/callback", OIDCCallbackHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/prov-1/callback?error=access_denied", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/login?error=access_denied" {
		t.Errorf("unexpected redirect: %s", loc)
	}
}

func TestOIDCCallbackHandler_MissingCodeOrState(t *testing.T) {
	setupTestDB(t)

	router := gin.New()
	router.GET("/api/v1/auth/oidc/:id/callback", OIDCCallbackHandler)

	tests := []struct {
		name  string
		query string
	}{
		{"missing both", ""},
		{"missing state", "code=abc"},
		{"missing code", "state=xyz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			url := "/api/v1/auth/oidc/prov-1/callback"
			if tt.query != "" {
				url += "?" + tt.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			router.ServeHTTP(w, req)

			if w.Code != http.StatusFound {
				t.Fatalf("expected 302, got %d", w.Code)
			}
			loc := w.Header().Get("Location")
			if loc != "/login?error=invalid_callback" {
				t.Errorf("unexpected redirect: %s", loc)
			}
		})
	}
}

func TestOIDCCallbackHandler_MissingStateCookie(t *testing.T) {
	setupTestDB(t)

	router := gin.New()
	router.GET("/api/v1/auth/oidc/:id/callback", OIDCCallbackHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/prov-1/callback?code=abc&state=xyz", nil)
	// No cookie set
	router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/login?error=missing_state" {
		t.Errorf("unexpected redirect: %s", loc)
	}
}

func TestOIDCAuthorizeHandler_ProviderNotFound(t *testing.T) {
	setupTestDB(t)

	router := gin.New()
	router.GET("/api/v1/auth/oidc/:id/authorize", OIDCAuthorizeHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/nonexistent/authorize", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
