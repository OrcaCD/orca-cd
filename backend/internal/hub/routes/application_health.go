package routes

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/healthhistory"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

var applicationHealthLog = logger.New("application-health", false)

// GetApplicationHealthHandler returns the availability statistics of an
// application for the requested range (24h, 7d or 30d; 24h by default).
var GetApplicationHealthHandler = restapi.Handler(huma.Operation{
	OperationID: "get-application-health",
	Method:      http.MethodGet,
	Path:        "/applications/{id}/health",
	Summary:     "Get the health statistics of an application",
	Description: "Returns the availability and the incidents of the application in the range. " +
		requires(models.ResourceApplication, models.ActionRead),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusNotFound, http.StatusForbidden},
}, func(ctx context.Context, input *struct {
	ID    string `path:"id" doc:"The ID of the application"`
	Range string `query:"range" default:"24h" doc:"One of 24h, 7d or 30d"`
}) (*struct{ Body healthhistory.Stats }, error) {
	applicationID := input.ID

	r, ok := healthhistory.ParseRange(input.Range)
	if !ok {
		return nil, huma.Error400BadRequest("invalid range: must be one of 24h, 7d, 30d")
	}

	if _, err := gorm.G[models.Application](db.DB).
		Select("id").
		Where("id = ?", applicationID).
		First(ctx); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		applicationHealthLog.Error().Err(err).Str("applicationId", applicationID).
			Msg("failed to load application for health statistics")
		return nil, internalError()
	}

	stats, err := healthhistory.GetStats(ctx, applicationID, r, time.Now())
	if err != nil {
		applicationHealthLog.Error().Err(err).Str("applicationId", applicationID).
			Msg("failed to compute application health statistics")
		return nil, internalError()
	}

	return &struct{ Body healthhistory.Stats }{Body: stats}, nil
})
