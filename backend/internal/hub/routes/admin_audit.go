package routes

import (
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
)

type auditLogResponse struct {
	Id         string             `json:"id"`
	Time       string             `json:"time"`
	EventType  string             `json:"eventType"`
	User       *adminUserResponse `json:"user"`
	TargetType string             `json:"targetType"`
	TargetId   *string            `json:"targetId"`
}

func AdminListAuditLogsHandler(c *gin.Context) {
	var auditLogs []models.AuditLog
	if err := db.DB.Order("created_at DESC").Limit(100).Preload("User").Find(&auditLogs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch audit logs"})
		return
	}

	responses := make([]auditLogResponse, len(auditLogs))
	for i, log := range auditLogs {
		var userResp *adminUserResponse

		if log.User != nil {
			u := toAdminUserResponse(log.User, nil)
			userResp = &u
		}

		responses[i] = auditLogResponse{
			Id:         log.Id,
			Time:       log.CreatedAt.Format(time.RFC3339),
			EventType:  log.EventType,
			User:       userResp,
			TargetType: log.TargetType,
			TargetId:   log.TargetId,
		}
	}

	c.JSON(http.StatusOK, responses)
}
