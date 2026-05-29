package routes

import (
	"fmt"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
)

type auditLogResponse struct {
	Id         string             `json:"id"`
	CreatedAt  string             `json:"createdAt"`
	EventType  string             `json:"eventType"`
	User       *adminUserResponse `json:"user"`
	TargetType string             `json:"targetType"`
	TargetId   *string            `json:"targetId"`
}

func AdminListAuditLogsHandler(c *gin.Context) {
	var auditLogs []models.AuditLog

	limit := 20
	if l := c.Query("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}

	cursor := c.Query("cursor")

	query := db.DB.Order("created_at DESC").Limit(limit + 1).Preload("User")

	if cursor != "" {
		t, err := time.Parse(time.RFC3339, cursor)
		if err == nil {
			query = query.Where("created_at < ?", t)
		}
	}

	if err := query.Find(&auditLogs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch audit logs"})
		return
	}

	hasMore := false
	if len(auditLogs) > limit {
		hasMore = true
		auditLogs = auditLogs[:limit]
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
			CreatedAt:  log.CreatedAt.Format(time.RFC3339),
			EventType:  log.EventType,
			User:       userResp,
			TargetType: log.TargetType,
			TargetId:   log.TargetId,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items":   responses,
		"hasMore": hasMore,
	})
}
