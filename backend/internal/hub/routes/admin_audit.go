package routes

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

const defaultAuditLogLimit = 20

const auditLogSearchCondition = "(event_type LIKE @search OR target_type LIKE @search OR target_id LIKE @search" +
	" OR user_id IN (SELECT id FROM users WHERE name LIKE @search OR email LIKE @search))"

type auditLogResponse struct {
	Id        string `json:"id" required:"true"`
	CreatedAt string `json:"createdAt" required:"true" format:"date-time"`
	EventType string `json:"eventType" required:"true" example:"created"`
	// Huma can't mark objects as nullable, so the field is documented as optional.
	User       *adminUserResponse `json:"user" doc:"Who made the change. null for system events and deleted users."`
	TargetType string             `json:"targetType" required:"true" example:"agent"`
	TargetId   *string            `json:"targetId" required:"true" nullable:"true"`
	// APITokenId is set when the action was made with an API token.
	APITokenId *string `json:"apiTokenId" required:"true" nullable:"true" doc:"The API token the change was made with. null for changes in the web UI."`
}

type auditLogsResponse struct {
	Items   []auditLogResponse `json:"items" required:"true"`
	HasMore bool               `json:"hasMore" required:"true" doc:"Whether there are older entries after these"`
}

// The paging parameters are strings because invalid values fall back to the
// default instead of being rejected.
type listAuditLogsInput struct {
	Limit  string `query:"limit" default:"20" doc:"The number of entries to return"`
	Offset string `query:"offset" default:"0" doc:"The number of entries to skip"`
	Search string `query:"search" doc:"Only returns entries whose event, target or user contains the text"`
}

var AdminListAuditLogsHandler = restapi.Handler(huma.Operation{
	OperationID: "list-audit-logs",
	Method:      http.MethodGet,
	Path:        "/admin/audit-logs",
	Summary:     "List audit log entries",
	Description: "Lists the audit log, newest first. " + requiresAdmin,
	Tags:        []string{tagAdmin},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusForbidden},
}, func(ctx context.Context, input *listAuditLogsInput) (*struct{ Body auditLogsResponse }, error) {
	limit := defaultAuditLogLimit
	if l := input.Limit; l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	offset := 0
	if o := input.Offset; o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	query := gorm.G[models.AuditLog](db.DB).Order("created_at DESC")
	if search := strings.TrimSpace(input.Search); search != "" {
		query = query.Where(auditLogSearchCondition, sql.Named("search", "%"+search+"%"))
	}

	query = query.
		Offset(offset).
		Limit(limit+1).
		Preload("User", func(db gorm.PreloadBuilder) error {
			return nil
		})

	auditLogs, err := query.Find(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to fetch audit logs")
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
			u := toAdminUserResponse(log.User, nil, nil)
			userResp = &u
		}

		responses[i] = auditLogResponse{
			Id:         log.Id,
			CreatedAt:  log.CreatedAt.Format(time.RFC3339),
			EventType:  log.EventType,
			User:       userResp,
			TargetType: log.TargetType,
			TargetId:   log.TargetId,
			APITokenId: log.APITokenId,
		}
	}

	return &struct{ Body auditLogsResponse }{Body: auditLogsResponse{Items: responses, HasMore: hasMore}}, nil
})
