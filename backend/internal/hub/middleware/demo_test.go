package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDemoBlocking_AllowsGet(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(DemoBlocking())
	router.GET("/demo", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/demo", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("expected body %q, got %q", "ok", w.Body.String())
	}
}

func TestDemoBlocking_BlocksNonGet(t *testing.T) {
	gin.SetMode(gin.TestMode)

	methods := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			router := gin.New()
			router.Use(DemoBlocking())
			router.Handle(method, "/demo", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/demo", nil)
			router.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
			}

			expectedBody := "This action is not allowed in demo mode."
			if w.Body.String() != expectedBody {
				t.Errorf("expected body %q, got %q", expectedBody, w.Body.String())
			}
		})
	}
}
