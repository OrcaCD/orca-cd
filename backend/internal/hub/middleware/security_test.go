package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurityHeaders_SetsAllHeaders(t *testing.T) {
	mw := SecurityHeaders()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	mw(c)

	expected := map[string]string{
		"X-Frame-Options":             "DENY",
		"X-Content-Type-Options":      "nosniff",
		"Referrer-Policy":             "same-origin",
		"X-Robots-Tag":               "noindex, nofollow, noarchive",
		"Cross-Origin-Opener-Policy":  "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Cross-Origin-Embedder-Policy": "require-corp",
	}

	for header, want := range expected {
		got := w.Header().Get(header)
		if got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}

	csp := w.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("Content-Security-Policy header should be set")
	}
}
