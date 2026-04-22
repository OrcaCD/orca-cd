package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/some-repo-id", nil)

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
