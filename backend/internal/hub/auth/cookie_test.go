package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c, w
}

func TestDefaultCookieConfig(t *testing.T) {
	cfg := defaultCookieConfig
	if cfg.Name == "" {
		t.Error("Name should not be empty")
	}
	if cfg.Path != "/" {
		t.Errorf("Path = %q, want %q", cfg.Path, "/")
	}
	if !cfg.Secure {
		t.Error("Secure should be true")
	}
	if !cfg.HttpOnly {
		t.Error("HttpOnly should be true")
	}
	if cfg.SameSite != http.SameSiteStrictMode {
		t.Error("SameSite should be Strict")
	}
	if cfg.MaxAge <= 0 {
		t.Errorf("MaxAge = %d, want positive value", cfg.MaxAge)
	}
}

func TestSetAuthCookie(t *testing.T) {
	c, w := newTestContext()
	SetAuthCookie(c, "test-token")

	cookie := findCookie(w.Result().Cookies(), defaultCookieConfig.Name)
	if cookie == nil {
		t.Fatalf("cookie %q not set in response", defaultCookieConfig.Name)
		return
	}
	if cookie.Value != "test-token" {
		t.Errorf("Value = %q, want %q", cookie.Value, "test-token")
	}
	if !cookie.HttpOnly {
		t.Error("HttpOnly should be true")
	}
	if !cookie.Secure {
		t.Error("Secure should be true")
	}
	if cookie.MaxAge != defaultCookieConfig.MaxAge {
		t.Errorf("MaxAge = %d, want %d", cookie.MaxAge, defaultCookieConfig.MaxAge)
	}
}

func TestClearAuthCookie(t *testing.T) {
	c, w := newTestContext()
	ClearAuthCookie(c)

	cookie := findCookie(w.Result().Cookies(), defaultCookieConfig.Name)
	if cookie == nil {
		t.Fatalf("cookie %q not set in response", defaultCookieConfig.Name)
		return
	}
	if cookie.Value != "" {
		t.Errorf("Value = %q, want empty", cookie.Value)
	}
	if cookie.MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1", cookie.MaxAge)
	}
}

func TestGetAuthCookie(t *testing.T) {
	c, _ := newTestContext()
	c.Request.AddCookie(&http.Cookie{Name: defaultCookieConfig.Name, Value: "my-token"}) //nolint:gosec

	token, err := GetAuthCookie(c)
	if err != nil {
		t.Fatalf("GetAuthCookie() error: %v", err)
	}
	if token != "my-token" {
		t.Errorf("GetAuthCookie() = %q, want %q", token, "my-token")
	}
}

func TestGetAuthCookie_Missing(t *testing.T) {
	c, _ := newTestContext()

	_, err := GetAuthCookie(c)
	if err == nil {
		t.Error("GetAuthCookie() expected error when cookie is absent")
	}
}

func TestPasskeyCookieLifecycle(t *testing.T) {
	c, w := newTestContext()
	SetPasskeyCookie(c, "ceremony-id", 5*time.Minute)
	cookie := findCookie(w.Result().Cookies(), CeremonyCookieName)
	if cookie == nil || cookie.Value != "ceremony-id" || cookie.Path != "/api/v1/auth" || cookie.MaxAge != 300 || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Domain != "" {
		t.Fatalf("unexpected ceremony cookie: %+v", cookie)
	}
	c.Request.AddCookie(cookie)
	if value, err := GetPasskeyCookie(c); err != nil || value != "ceremony-id" {
		t.Fatalf("could not read ceremony cookie: %q %v", value, err)
	}
	c, w = newTestContext()
	ClearPasskeyCookie(c)
	cleared := findCookie(w.Result().Cookies(), CeremonyCookieName)
	if cleared == nil || cleared.Value != "" || cleared.Path != cookie.Path || cleared.MaxAge != -1 || !cleared.Secure || !cleared.HttpOnly || cleared.SameSite != cookie.SameSite {
		t.Fatalf("ceremony cookie was not cleared with the same scope: %+v", cleared)
	}
	if _, err := GetPasskeyCookie(c); err == nil {
		t.Fatal("accepted a missing ceremony cookie")
	}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}
