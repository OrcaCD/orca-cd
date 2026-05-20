package utils

import (
	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/gin-gonic/gin"
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

	Log.Info().
		Str("user", userIdDisplay).
		Str("event", eventType).
		Str("target", targetType).
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

	if err := db.DB.Create(&audit).Error; err != nil {
		Log.Error().Err(err).Msg("Failed to write audit log to database")
	}
}
