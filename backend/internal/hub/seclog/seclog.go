// Package seclog writes security events to the Hub log in a stable format, so
// intrusion detection tools like CrowdSec or fail2ban can parse them.
package seclog

import (
	"sync/atomic"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

const (
	LoginFailed          = "login_failed"
	LoginSuccess         = "login_success"
	RegistrationFailed   = "registration_failed"
	RegistrationSuccess  = "registration_success"
	SetupCodeFailed      = "setup_code_failed"
	PasswordChangeFailed = "password_change_failed"
	OIDCLoginFailed      = "oidc_login_failed"
	OIDCLoginSuccess     = "oidc_login_success"
	TokenRejected        = "token_rejected"
	AccessDenied         = "access_denied"
	AgentAuthFailed      = "agent_auth_failed"
	WebhookAuthFailed    = "webhook_auth_failed"
)

const message = "security event"

var current atomic.Pointer[zerolog.Logger]

func init() {
	Init(logger.New("hub", false))
}

func Init(l zerolog.Logger) {
	l = l.With().Str("category", "security").Logger()
	current.Store(&l)
}

func Warn(c *gin.Context, event string, fields ...any) {
	write(c, current.Load().Warn(), event, fields)
}

func Info(c *gin.Context, event string, fields ...any) {
	write(c, current.Load().Info(), event, fields)
}

func write(c *gin.Context, e *zerolog.Event, event string, fields []any) {
	e.Str("event", event).
		Str("client_ip", c.ClientIP()).
		Str("method", c.Request.Method).
		Str("path", c.Request.URL.Path)
	if user, ok := auth.GetUser(c); ok {
		e.Str("user_id", user.Id)
	}
	e.Fields(fields).Msg(message)
}
