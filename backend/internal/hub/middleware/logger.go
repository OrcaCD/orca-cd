package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func RequestLogger(logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {

		c.Next()

		status := c.Writer.Status()
		var level zerolog.Level
		switch {
		case status >= http.StatusInternalServerError:
			level = zerolog.ErrorLevel
		case status >= http.StatusBadRequest:
			level = zerolog.DebugLevel
		default:
			level = zerolog.DebugLevel
		}

		logger.WithLevel(level).
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Int("status", status).
			Str("client_ip", c.ClientIP()).
			Msg("request")
	}
}

func Recovery(logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				logger.Error().
					Interface("error", err).
					Str("method", c.Request.Method).
					Str("path", c.Request.URL.Path).
					Stack().
					Msg("panic recovered")
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}
