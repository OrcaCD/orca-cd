package routes

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const defaultAuditLogLimit = 20

var auditLogSearchCondition = fmt.Sprintf(
	`(%s LIKE @enum ESCAPE '\' OR %s LIKE @enum ESCAPE '\' OR target_id LIKE @text ESCAPE '\'`+
		` OR user_id IN (SELECT id FROM users WHERE name LIKE @text ESCAPE '\' OR email LIKE @text ESCAPE '\'))`,
	enumSearchColumn("event_type"), enumSearchColumn("target_type"),
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
	ctx := c.Request.Context()

	limit := defaultAuditLogLimit
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}

	offset := 0
	if o := c.Query("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil {
			offset = v
		}
	}

	query := gorm.G[models.AuditLog](db.DB).Order("created_at DESC")
	if search, ok := searchQuery(c); ok {
		query = query.Where(auditLogSearchCondition, map[string]any{"text": search.Text, "enum": search.Enum})
	}

	query = query.
		Offset(offset).
		Limit(limit+1).
		Preload("User", func(db gorm.PreloadBuilder) error {
			return nil
		})

	auditLogs, err := query.Find(ctx)
	if err != nil {
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
