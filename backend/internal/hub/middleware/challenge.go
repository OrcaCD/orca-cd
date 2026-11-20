package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BearerChallenge adds a WWW-Authenticate header to the 401 responses of the
// route, which tells clients how to get a token (RFC 6750, RFC 9728).
func BearerChallenge(challenge string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer = &challengeWriter{ResponseWriter: c.Writer, challenge: challenge}
		c.Next()
	}
}

type challengeWriter struct {
	gin.ResponseWriter
	challenge string
}

func (w *challengeWriter) WriteHeader(code int) {
	if code == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", w.challenge)
	}
	w.ResponseWriter.WriteHeader(code)
}
