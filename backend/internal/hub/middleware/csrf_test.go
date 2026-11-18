package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/apitoken"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestValidateOrigin_SafeMethodsPassThrough(t *testing.T) {
	mw := ValidateOrigin("https://example.com")

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(method, "/", nil)

			mw(c)

			if w.Code != http.StatusOK {
				t.Errorf("expected 200 for %s, got %d", method, w.Code)
			}
		})
	}
}

func TestValidateOrigin_MissingOriginHeader(t *testing.T) {
	mw := ValidateOrigin("https://example.com")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	mw(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if !c.IsAborted() {
		t.Error("expected request to be aborted")
	}
}

func TestValidateOrigin_WrongOrigin(t *testing.T) {
	mw := ValidateOrigin("https://example.com")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Request.Header.Set("Origin", "https://evil.com")

	mw(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if !c.IsAborted() {
		t.Error("expected request to be aborted")
	}
}

func TestValidateOrigin_CorrectOrigin(t *testing.T) {
	mw := ValidateOrigin("https://example.com")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Request.Header.Set("Origin", "https://example.com")

	mw(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if c.IsAborted() {
		t.Error("request should not be aborted")
	}
}

func TestValidateOrigin_OriginWithTrailingSlash(t *testing.T) {
	mw := ValidateOrigin("https://example.com")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Request.Header.Set("Origin", "https://example.com/")

	mw(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestValidateOrigin_WebhookRoute_NoOriginAllowed(t *testing.T) {
	mw := ValidateOrigin("https://example.com")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/repositories/some-repo-id", nil)

	mw(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected webhook POST without Origin to pass (200), got %d", w.Code)
	}
	if c.IsAborted() {
		t.Error("webhook request should not be aborted")
	}
}

func TestValidateOrigin_PutDeletePatch(t *testing.T) {
	mw := ValidateOrigin("https://example.com")

	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method+"_no_origin", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(method, "/", nil)

			mw(c)

			if w.Code != http.StatusForbidden {
				t.Errorf("expected 403 for %s without origin, got %d", method, w.Code)
			}
		})
	}
}

func TestValidateOrigin_GitHubActionsRoute_NoOriginAllowed(t *testing.T) {
	mw := ValidateOrigin("https://example.com")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/github-actions", nil)

	mw(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected GitHub Actions POST without Origin to pass (200), got %d", w.Code)
	}
	if c.IsAborted() {
		t.Error("GitHub Actions request should not be aborted")
	}
}

func TestValidateOrigin_GitHubActionsRoute_WrongOriginAllowed(t *testing.T) {
	mw := ValidateOrigin("https://example.com")

	// CSRF validation is skipped entirely for this path, so a mismatched Origin
	// must not block the request either.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/github-actions", nil)
	c.Request.Header.Set("Origin", "https://evil.com")

	mw(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected GitHub Actions POST with wrong Origin to pass (200), got %d", w.Code)
	}
	if c.IsAborted() {
		t.Error("GitHub Actions request should not be aborted")
	}
}

func TestValidateOrigin_APIToken(t *testing.T) {
	mw := ValidateOrigin("https://example.com")

	tests := []struct {
		name        string
		token       string
		cookie      bool
		wantAborted bool
	}{
		{"api token without origin", apitoken.Generate().Plain, false, false},
		{"api token with session cookie", apitoken.Generate().Plain, true, true},
		{"malformed token", "orca_short", false, true},
		{"jwt", "header.payload.signature", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/applications", nil)
			c.Request.Header.Set("Authorization", "Bearer "+tt.token)
			if tt.cookie {
				c.Request.AddCookie(&http.Cookie{Name: "orcacd_auth", Value: "session"}) //nolint:gosec
			}

			mw(c)

			if c.IsAborted() != tt.wantAborted {
				t.Errorf("expected aborted=%v, got %v (status %d)", tt.wantAborted, c.IsAborted(), w.Code)
			}
		})
	}
}
