package utils

import (
	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var Log = logger.New("audit-log", false)

func RecordAuditLog(c *gin.Context, eventType string, targetType string, targetId string) {
	var userIdPtr *string

	if claims, ok := auth.GetClaims(c); ok {
		userIdPtr = &claims.Subject
	}

	userIdDisplay := "system"
	if userIdPtr != nil {
		userIdDisplay = *userIdPtr
	}

	Log.Debug().
		Str("user", userIdDisplay).
		Str("event", eventType).
		Str("target", targetType).
		Str("targetId", targetId).
		Msg("Recording audit log")

	var targetIdPtr *string
	if targetId != "" {
		targetIdPtr = &targetId
	}

	audit := models.AuditLog{
		EventType:  eventType,
		UserId:     userIdPtr,
		TargetType: targetType,
		TargetId:   targetIdPtr,
	}

	if err := gorm.G[models.AuditLog](db.DB).Create(c.Request.Context(), &audit); err != nil {
		Log.Error().Err(err).Msg("Failed to write audit log to database")
	}
}
