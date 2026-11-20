package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBearerChallenge_OnlyOnUnauthorized(t *testing.T) {
	const challenge = `Bearer resource_metadata="https://example.com/.well-known/oauth-protected-resource"`
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusForbidden} {
		router := gin.New()
		router.GET("/", BearerChallenge(challenge), func(c *gin.Context) {
			c.JSON(status, gin.H{})
		})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

		got := w.Header().Get("WWW-Authenticate")
		if want := status == http.StatusUnauthorized; (got == challenge) != want {
			t.Errorf("status %d: WWW-Authenticate = %q", status, got)
		}
	}
}
