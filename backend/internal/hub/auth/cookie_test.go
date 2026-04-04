package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
	c.Request.AddCookie(&http.Cookie{Name: defaultCookieConfig.Name, Value: "my-token"})

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

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}
