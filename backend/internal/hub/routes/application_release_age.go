package routes

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	hubApplications "github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/releaseage"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var releaseAgeLog = logger.New("release-age", false)

const (
	maxMinimumReleaseAgeSeconds    = 365 * 24 * 60 * 60
	maxReleaseAgeAllowedRegistries = 50
	maxRegistryLength              = 253
)

// releaseAgeSettings are the release age settings of an application. A nil
// MinimumSeconds uses the global default.
type releaseAgeSettings struct {
	MinimumSeconds    *int64           `json:"minimumSeconds" required:"true" nullable:"true" doc:"The minimum release age of the images, null uses the global one"`
	AllowedRegistries []string         `json:"allowedRegistries" required:"true" doc:"Registry hosts whose images skip the minimum release age of the application"`
	ServiceOverrides  map[string]int64 `json:"serviceOverrides" required:"true" doc:"The minimum release age of single compose services, by service name"`
}

type globalReleaseAgeSettings struct {
	MinimumSeconds    int64    `json:"minimumSeconds" required:"true" doc:"The default minimum release age of applications and the least one of every image not from an allowed registry"`
	AllowedRegistries []string `json:"allowedRegistries" required:"true" doc:"Registry hosts whose images skip the global minimum release age"`
}

func toReleaseAgeSettings(app *models.Application) releaseAgeSettings {
	overrides := app.ReleaseAgeServiceOverrides
	if overrides == nil {
		overrides = map[string]int64{}
	}
	return releaseAgeSettings{
		MinimumSeconds:    app.MinimumReleaseAgeSeconds,
		AllowedRegistries: nonNil(app.ReleaseAgeAllowedRegistries),
		ServiceOverrides:  overrides,
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func validMinimumSeconds(seconds int64) bool {
	return seconds >= 0 && seconds <= maxMinimumReleaseAgeSeconds
}

// normalizeRegistries lowercases and deduplicates registry hosts like
// "registry.example.com:5000". It reports false for an invalid host.
func normalizeRegistries(registries []string) ([]string, bool) {
	if len(registries) > maxReleaseAgeAllowedRegistries {
		return nil, false
	}
	normalized := make([]string, 0, len(registries))
	for _, registry := range registries {
		registry = strings.ToLower(strings.TrimSpace(registry))
		if registry == "" || len(registry) > maxRegistryLength || strings.ContainsAny(registry, "/ \t@") {
			return nil, false
		}
		if !slices.Contains(normalized, registry) {
			normalized = append(normalized, registry)
		}
	}
	return normalized, true
}

// checkMinimumSeconds returns a 400 if seconds is not a valid minimum release
// age, which must not be below the global one.
func checkMinimumSeconds(seconds *int64, global models.Settings) error {
	if seconds == nil {
		return nil
	}
	if !validMinimumSeconds(*seconds) {
		return huma.Error400BadRequest("minimum release age must be between 0 and 365 days")
	}
	if *seconds < global.MinimumReleaseAgeSeconds {
		return huma.Error400BadRequest("minimum release age must not be below the global minimum release age")
	}
	return nil
}

// checkReleaseAge checks seconds against the global settings when it is set.
func checkReleaseAge(ctx context.Context, seconds *int64) error {
	global, err := releaseage.Global(ctx)
	if err != nil {
		return internalError()
	}
	return checkMinimumSeconds(seconds, global)
}

const invalidRegistriesMessage = "allowed registries must be at most 50 registry hosts, e.g. registry.example.com:5000"

// The minimum seconds are required but nullable: null uses the global
// minimum release age, while a missing field is refused.
type updateReleaseAgeRequest struct {
	MinimumSeconds    *int64   `json:"minimumSeconds" required:"true" nullable:"true" doc:"The minimum release age in seconds, at most 365 days and not below the global one. null uses the global one."`
	AllowedRegistries []string `json:"allowedRegistries" required:"true" nullable:"false" doc:"At most 50 registry hosts, e.g. registry.example.com:5000, whose images skip the minimum release age"`
}

// UpdateReleaseAgeSettingsHandler configures the minimum release age of the
// images an application is updated to. A null minimumSeconds uses the global
// one, which is also the least minimum release age.
var UpdateReleaseAgeSettingsHandler = restapi.Handler(huma.Operation{
	OperationID: "update-application-release-age-settings",
	Method:      http.MethodPut,
	Path:        "/applications/{id}/release-age/settings",
	Summary:     "Update the release age settings of an application",
	Description: "Sets how old an image must be before the application is updated to it. " + requires(models.ResourceApplication, models.ActionUpdate),
	Tags:        []string{tagApplications},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusForbidden},
}, func(ctx context.Context, input *struct {
	ID   string `path:"id" doc:"The ID of the application"`
	Body updateReleaseAgeRequest
}) (*applicationOutput, error) {
	id, req := input.ID, input.Body

	if err := checkReleaseAge(ctx, req.MinimumSeconds); err != nil {
		return nil, err
	}
	registries, ok := normalizeRegistries(req.AllowedRegistries)
	if !ok {
		return nil, huma.Error400BadRequest(invalidRegistriesMessage)
	}

	rowsAffected, err := gorm.G[models.Application](db.DB).
		Where("id = ?", id).
		Select("MinimumReleaseAgeSeconds", "ReleaseAgeAllowedRegistries").
		Updates(ctx, models.Application{
			MinimumReleaseAgeSeconds:    req.MinimumSeconds,
			ReleaseAgeAllowedRegistries: registries,
		})
	if err != nil {
		return nil, internalError()
	}
	if rowsAffected == 0 {
		return nil, huma.Error404NotFound("application not found")
	}
	return updatedApplication(restapi.GinContext(ctx), id)
})

const (
	maxServiceReleaseAgeOverrides = 100
	maxServiceNameLength          = 255
)

type updateServiceReleaseAgeRequest struct {
	MinimumSeconds *int64 `json:"minimumSeconds" required:"true" nullable:"true" doc:"The minimum release age of the service in seconds, at most 365 days and not below the global one. null removes the override."`
}

// serviceOverridesMu serializes changes to the service overrides, which are
// read, changed and written back as a whole.
var serviceOverridesMu sync.Mutex

// UpdateServiceReleaseAgeHandler overrides the minimum release age of one
// compose service. A null minimumSeconds removes the override.
var UpdateServiceReleaseAgeHandler = restapi.Handler(huma.Operation{
	OperationID: "update-service-release-age",
	Method:      http.MethodPut,
	Path:        "/applications/{id}/release-age/services/{service}",
	Summary:     "Override the release age of a service",
	Description: "Sets the minimum release age of the images of one compose service of an application, at most 100 services can have one. " +
		requires(models.ResourceApplication, models.ActionUpdate),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusNotFound, http.StatusForbidden},
}, func(ctx context.Context, input *struct {
	ID      string `path:"id" doc:"The ID of the application"`
	Service string `path:"service" doc:"The name of the compose service"`
	Body    updateServiceReleaseAgeRequest
}) (*applicationOutput, error) {
	id, service, req := input.ID, input.Service, input.Body

	if service == "" || len(service) > maxServiceNameLength {
		return nil, huma.Error400BadRequest("invalid request")
	}
	if err := checkReleaseAge(ctx, req.MinimumSeconds); err != nil {
		return nil, err
	}

	serviceOverridesMu.Lock()
	defer serviceOverridesMu.Unlock()

	application, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		return nil, internalError()
	}

	overrides := maps.Clone(application.ReleaseAgeServiceOverrides)
	if overrides == nil {
		overrides = map[string]int64{}
	}
	if req.MinimumSeconds == nil {
		delete(overrides, service)
	} else {
		if _, exists := overrides[service]; !exists && len(overrides) >= maxServiceReleaseAgeOverrides {
			return nil, huma.Error400BadRequest("the minimum release age can be overridden for at most 100 services")
		}
		overrides[service] = *req.MinimumSeconds
	}
	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ?", id).
		Select("ReleaseAgeServiceOverrides").
		Updates(ctx, models.Application{ReleaseAgeServiceOverrides: overrides}); err != nil {
		return nil, internalError()
	}
	return updatedApplication(restapi.GinContext(ctx), id)
})

// updatedApplication returns the application after its release age settings
// changed and sends them to its agent.
func updatedApplication(c *gin.Context, id string) (*applicationOutput, error) {
	application, err := gorm.G[models.Application](db.DB).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Where("id = ?", id).
		First(c.Request.Context())
	if err != nil {
		return nil, internalError()
	}

	utils.RecordAuditLog(c, "updated", "application", id)
	sse.PublishUpdate(sse.TopicApplications.For(id))
	sendAgentSettings(c, application.AgentId)
	hubApplications.RecheckWaitingImages(c.Request.Context(), id)
	return &applicationOutput{Body: toApplicationResponse(&application)}, nil
}

type globalReleaseAgeOutput struct {
	Body globalReleaseAgeSettings
}

// GetReleaseAgeSettingsHandler returns the global release age settings.
var GetReleaseAgeSettingsHandler = restapi.Handler(huma.Operation{
	OperationID: "get-release-age-settings",
	Method:      http.MethodGet,
	Path:        "/settings/release-age",
	Summary:     "Get the global release age settings",
	Description: "Returns the default minimum release age of applications and the registries that skip it.",
	Tags:        []string{tagApplications},
	Security:    restapi.Authenticated,
}, func(ctx context.Context, _ *struct{}) (*globalReleaseAgeOutput, error) {
	settings, err := releaseage.Global(ctx)
	if err != nil {
		return nil, internalError()
	}
	return &globalReleaseAgeOutput{Body: globalReleaseAgeSettings{
		MinimumSeconds:    settings.MinimumReleaseAgeSeconds,
		AllowedRegistries: nonNil(settings.ReleaseAgeAllowedRegistries),
	}}, nil
})

type updateGlobalReleaseAgeRequest struct {
	MinimumSeconds    int64    `json:"minimumSeconds" required:"true" doc:"The minimum release age in seconds, at most 365 days"`
	AllowedRegistries []string `json:"allowedRegistries" required:"true" nullable:"false" doc:"At most 50 registry hosts, e.g. registry.example.com:5000, whose images skip the global minimum release age"`
}

// AdminUpdateReleaseAgeSettingsHandler changes the global release age settings.
// The minimum release age is the default of applications without their own,
// and the least one of every image that is not from an allowed registry.
var AdminUpdateReleaseAgeSettingsHandler = restapi.Handler(huma.Operation{
	OperationID: "update-release-age-settings",
	Method:      http.MethodPut,
	Path:        "/admin/settings/release-age",
	Summary:     "Update the global release age settings",
	Description: "Sets the default minimum release age of applications, which is also the least one of every image not from an allowed registry. " +
		"Application and service ages below a raised minimum are raised to it. " + requiresAdmin,
	Tags:     []string{tagAdmin},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusForbidden},
}, func(ctx context.Context, input *struct {
	Body updateGlobalReleaseAgeRequest
}) (*globalReleaseAgeOutput, error) {
	c := restapi.GinContext(ctx)
	req := input.Body

	if !validMinimumSeconds(req.MinimumSeconds) {
		return nil, huma.Error400BadRequest("minimum release age must be between 0 and 365 days")
	}
	registries, ok := normalizeRegistries(req.AllowedRegistries)
	if !ok {
		return nil, huma.Error400BadRequest(invalidRegistriesMessage)
	}

	settings := models.Settings{
		Id:                          models.SettingsId,
		MinimumReleaseAgeSeconds:    req.MinimumSeconds,
		ReleaseAgeAllowedRegistries: registries,
	}
	// The overrides are read and written back as a whole, see UpdateServiceReleaseAgeHandler.
	serviceOverridesMu.Lock()
	var raised []string
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := gorm.G[models.Settings](tx, clause.OnConflict{UpdateAll: true}).Create(ctx, &settings); err != nil {
			return err
		}
		var err error
		raised, err = raiseReleaseAgesBelow(ctx, tx, settings.MinimumReleaseAgeSeconds)
		return err
	})
	serviceOverridesMu.Unlock()
	if err != nil {
		releaseAgeLog.Error().Err(err).Msg("failed to update the global release age settings")
		return nil, internalError()
	}

	utils.RecordAuditLog(c, "updated", "settings", "release-age")
	sse.PublishUpdate(sse.TopicSettings)
	for _, id := range raised {
		sse.PublishUpdate(sse.TopicApplications.For(id))
	}
	sendAllAgentSettings(c)
	hubApplications.RecheckWaitingImages(ctx)
	return &globalReleaseAgeOutput{Body: globalReleaseAgeSettings{MinimumSeconds: settings.MinimumReleaseAgeSeconds, AllowedRegistries: registries}}, nil
})

// raiseReleaseAgesBelow makes the stored ages of applications and services
// match a raised global minimum, so they show and save the age that applies.
// An application age below it uses the global one again, and a service age
// below it is raised to it. It returns the applications it changed.
func raiseReleaseAgesBelow(ctx context.Context, tx *gorm.DB, minimumSeconds int64) ([]string, error) {
	applications, err := gorm.G[models.Application](tx).
		Select("id", "minimum_release_age_seconds", "release_age_service_overrides").
		Find(ctx)
	if err != nil {
		return nil, err
	}
	var raised []string
	for _, application := range applications {
		ownAge := application.MinimumReleaseAgeSeconds
		changed := ownAge != nil && *ownAge < minimumSeconds
		if changed {
			ownAge = nil
		}
		overrides := maps.Clone(application.ReleaseAgeServiceOverrides)
		if overrides == nil {
			overrides = map[string]int64{}
		}
		for service, seconds := range overrides {
			if seconds < minimumSeconds {
				overrides[service] = minimumSeconds
				changed = true
			}
		}
		if !changed {
			continue
		}
		if _, err := gorm.G[models.Application](tx).
			Where("id = ?", application.Id).
			Select("MinimumReleaseAgeSeconds", "ReleaseAgeServiceOverrides").
			Updates(ctx, models.Application{MinimumReleaseAgeSeconds: ownAge, ReleaseAgeServiceOverrides: overrides}); err != nil {
			return nil, err
		}
		raised = append(raised, application.Id)
	}
	return raised, nil
}

// sendAllAgentSettings pushes the settings to every agent, as global settings
// apply to all of them.
func sendAllAgentSettings(c *gin.Context) {
	agentIDs, err := gorm.G[models.Application](db.DB).Distinct("agent_id").Find(c.Request.Context())
	if err != nil {
		releaseAgeLog.Error().Err(err).Msg("failed to list the agents to send the release age settings to")
		return
	}
	for _, app := range agentIDs {
		sendAgentSettings(c, app.AgentId)
	}
}

type imageUpdateResponse struct {
	Service           string  `json:"service" required:"true"`
	Image             string  `json:"image" required:"true"`
	Status            string  `json:"status" required:"true"`
	CurrentDigest     string  `json:"currentDigest" required:"true"`
	CurrentVersion    string  `json:"currentVersion" required:"true"`
	NewDigest         string  `json:"newDigest" required:"true"`
	NewVersion        string  `json:"newVersion" required:"true"`
	PushedAt          *string `json:"pushedAt" required:"true" nullable:"true" format:"date-time"`
	FirstSeen         bool    `json:"firstSeen" required:"true"`
	EligibleAt        *string `json:"eligibleAt" required:"true" nullable:"true" format:"date-time" doc:"When the new image is old enough to update to"`
	MinimumAgeSeconds int64   `json:"minimumAgeSeconds" required:"true"`
	// AgeSource says where the minimum age comes from: label, service,
	// application or global.
	AgeSource string `json:"ageSource" required:"true" enum:"label,service,application,global" doc:"Where the minimum age comes from"`
	// MinimumAgeEnforced means the global minimum raised the age of its source.
	MinimumAgeEnforced bool   `json:"minimumAgeEnforced" required:"true" doc:"The global minimum release age raised the age of its source"`
	Reason             string `json:"reason" required:"true"`
}

type imageUpdatesResponse struct {
	CheckedAt *string               `json:"checkedAt" required:"true" nullable:"true" format:"date-time" doc:"When the images were last checked"`
	Images    []imageUpdateResponse `json:"images" required:"true"`
}

// ListApplicationImageUpdatesHandler returns the update state of every image
// of the application, as of the last image check.
var ListApplicationImageUpdatesHandler = restapi.Handler(huma.Operation{
	OperationID: "list-application-image-updates",
	Method:      http.MethodGet,
	Path:        "/applications/{id}/image-updates",
	Summary:     "List the image updates of an application",
	Description: "Returns the update state of every image of the application as of the last image check, " +
		"including updates held back by the minimum release age. " + requires(models.ResourceApplication, models.ActionRead),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusNotFound, http.StatusForbidden},
}, func(ctx context.Context, input *struct {
	ID string `path:"id" doc:"The ID of the application"`
}) (*struct{ Body imageUpdatesResponse }, error) {
	id := input.ID

	application, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		return nil, internalError()
	}
	rows, err := gorm.G[models.ApplicationImageUpdate](db.DB).
		Where("application_id = ?", id).
		Order("service").
		Find(ctx)
	if err != nil {
		return nil, internalError()
	}
	global, err := releaseage.Global(ctx)
	if err != nil {
		return nil, internalError()
	}
	_, inherited := releaseage.MinimumSeconds(&application, global)

	response := imageUpdatesResponse{Images: make([]imageUpdateResponse, 0, len(rows))}
	var checkedAt time.Time
	for i := range rows {
		row := &rows[i]
		if row.CheckedAt.After(checkedAt) {
			checkedAt = row.CheckedAt
		}
		response.Images = append(response.Images, imageUpdateResponse{
			Service:            row.Service,
			Image:              row.Image,
			Status:             string(row.Status),
			CurrentDigest:      row.CurrentDigest,
			CurrentVersion:     row.CurrentVersion,
			NewDigest:          row.NewDigest,
			NewVersion:         row.NewVersion,
			PushedAt:           formatTimestamp(row.PushedAt),
			FirstSeen:          row.FirstSeen,
			EligibleAt:         formatTimestamp(row.EligibleAt),
			MinimumAgeSeconds:  row.MinimumAgeSeconds,
			AgeSource:          ageSource(row, &application, inherited),
			MinimumAgeEnforced: row.MinimumAgeEnforced,
			Reason:             row.Reason,
		})
	}
	if !checkedAt.IsZero() {
		response.CheckedAt = formatTimestamp(&checkedAt)
	}
	return &struct{ Body imageUpdatesResponse }{Body: response}, nil
})

func ageSource(row *models.ApplicationImageUpdate, app *models.Application, inherited bool) string {
	// A label wins over the settings, even when the global minimum raises it.
	if row.MinimumAgeFromLabel {
		return "label"
	}
	if row.MinimumAgeEnforced {
		return "global"
	}
	if _, ok := app.ReleaseAgeServiceOverrides[row.Service]; ok {
		return "service"
	}
	if inherited {
		return "global"
	}
	return "application"
}
