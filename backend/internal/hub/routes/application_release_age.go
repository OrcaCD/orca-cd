package routes

import (
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/releaseage"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxMinimumReleaseAgeSeconds    = 365 * 24 * 60 * 60
	maxReleaseAgeAllowedRegistries = 50
	maxRegistryLength              = 253
)

// releaseAgeSettings are the release age settings of an application. A nil
// MinimumSeconds uses the global default.
type releaseAgeSettings struct {
	MinimumSeconds    *int64           `json:"minimumSeconds"`
	AllowedRegistries []string         `json:"allowedRegistries"`
	ServiceOverrides  map[string]int64 `json:"serviceOverrides"`
}

type globalReleaseAgeSettings struct {
	MinimumSeconds    int64    `json:"minimumSeconds"`
	AllowedRegistries []string `json:"allowedRegistries"`
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

type updateReleaseAgeRequest struct {
	MinimumSeconds    *int64   `json:"minimumSeconds"`
	AllowedRegistries []string `json:"allowedRegistries"`
}

// UpdateReleaseAgeSettingsHandler configures the minimum release age of the
// images an application is updated to. A null minimumSeconds uses the global default.
func UpdateReleaseAgeSettingsHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var req updateReleaseAgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.MinimumSeconds != nil && !validMinimumSeconds(*req.MinimumSeconds) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "minimum release age must be between 0 and 365 days"})
		return
	}
	registries, ok := normalizeRegistries(req.AllowedRegistries)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "allowed registries must be at most 50 registry hosts, e.g. registry.example.com:5000"})
		return
	}

	rowsAffected, err := gorm.G[models.Application](db.DB).
		Where("id = ?", id).
		Select("MinimumReleaseAgeSeconds", "ReleaseAgeAllowedRegistries").
		Updates(ctx, models.Application{
			MinimumReleaseAgeSeconds:    req.MinimumSeconds,
			ReleaseAgeAllowedRegistries: registries,
		})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
		return
	}
	respondWithUpdatedApplication(c, id)
}

type updateServiceReleaseAgeRequest struct {
	MinimumSeconds *int64 `json:"minimumSeconds"`
}

// UpdateServiceReleaseAgeHandler overrides the minimum release age of one
// compose service. A null minimumSeconds removes the override.
func UpdateServiceReleaseAgeHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")
	service := c.Param("service")

	var req updateServiceReleaseAgeRequest
	if err := c.ShouldBindJSON(&req); err != nil || service == "" || len(service) > 255 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.MinimumSeconds != nil && !validMinimumSeconds(*req.MinimumSeconds) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "minimum release age must be between 0 and 365 days"})
		return
	}

	application, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	overrides := maps.Clone(application.ReleaseAgeServiceOverrides)
	if overrides == nil {
		overrides = map[string]int64{}
	}
	if req.MinimumSeconds == nil {
		delete(overrides, service)
	} else {
		overrides[service] = *req.MinimumSeconds
	}
	if _, err := gorm.G[models.Application](db.DB).
		Where("id = ?", id).
		Select("ReleaseAgeServiceOverrides").
		Updates(ctx, models.Application{ReleaseAgeServiceOverrides: overrides}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	respondWithUpdatedApplication(c, id)
}

// respondWithUpdatedApplication answers with the application after its
// release age settings changed and sends them to its agent.
func respondWithUpdatedApplication(c *gin.Context, id string) {
	application, err := gorm.G[models.Application](db.DB).
		Preload("Repository", nil).
		Preload("Agent", nil).
		Where("id = ?", id).
		First(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	utils.RecordAuditLog(c, "updated", "application", id)
	c.JSON(http.StatusOK, toApplicationResponse(&application))
	sse.PublishUpdate(sse.TopicApplications)
	sendAgentSettings(c, application.AgentId)
}

// GetReleaseAgeSettingsHandler returns the global release age settings.
func GetReleaseAgeSettingsHandler(c *gin.Context) {
	settings, err := releaseage.Global(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, globalReleaseAgeSettings{
		MinimumSeconds:    settings.MinimumReleaseAgeSeconds,
		AllowedRegistries: nonNil(settings.ReleaseAgeAllowedRegistries),
	})
}

// AdminUpdateReleaseAgeSettingsHandler changes the global release age settings,
// which apply to every application without its own minimum release age.
func AdminUpdateReleaseAgeSettingsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	var req globalReleaseAgeSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if !validMinimumSeconds(req.MinimumSeconds) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "minimum release age must be between 0 and 365 days"})
		return
	}
	registries, ok := normalizeRegistries(req.AllowedRegistries)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "allowed registries must be at most 50 registry hosts, e.g. registry.example.com:5000"})
		return
	}

	settings := models.Settings{
		Id:                          models.SettingsId,
		MinimumReleaseAgeSeconds:    req.MinimumSeconds,
		ReleaseAgeAllowedRegistries: registries,
	}
	if err := db.DB.WithContext(ctx).Save(&settings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	utils.RecordAuditLog(c, "updated", "settings", "release-age")
	c.JSON(http.StatusOK, globalReleaseAgeSettings{MinimumSeconds: settings.MinimumReleaseAgeSeconds, AllowedRegistries: registries})
	sse.PublishUpdate(sse.TopicSettings)
	sendAllAgentSettings(c)
}

// sendAllAgentSettings pushes the settings to every agent, as global settings
// apply to all of them.
func sendAllAgentSettings(c *gin.Context) {
	agentIDs, err := gorm.G[models.Application](db.DB).Distinct("agent_id").Find(c.Request.Context())
	if err != nil {
		return
	}
	for _, app := range agentIDs {
		sendAgentSettings(c, app.AgentId)
	}
}

type imageUpdateResponse struct {
	Service           string  `json:"service"`
	Image             string  `json:"image"`
	Status            string  `json:"status"`
	CurrentDigest     string  `json:"currentDigest"`
	CurrentVersion    string  `json:"currentVersion"`
	NewDigest         string  `json:"newDigest"`
	NewVersion        string  `json:"newVersion"`
	PushedAt          *string `json:"pushedAt"`
	FirstSeen         bool    `json:"firstSeen"`
	EligibleAt        *string `json:"eligibleAt"`
	MinimumAgeSeconds int64   `json:"minimumAgeSeconds"`
	// AgeSource says where the minimum age comes from: label, service,
	// application or global.
	AgeSource string `json:"ageSource"`
	Reason    string `json:"reason"`
}

type imageUpdatesResponse struct {
	CheckedAt *string               `json:"checkedAt"`
	Images    []imageUpdateResponse `json:"images"`
}

// ListApplicationImageUpdatesHandler returns the update state of every image
// of the application, as of the last image check.
func ListApplicationImageUpdatesHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	application, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	rows, err := gorm.G[models.ApplicationImageUpdate](db.DB).
		Where("application_id = ?", id).
		Order("service").
		Find(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	global, err := releaseage.Global(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
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
			Service:           row.Service,
			Image:             row.Image,
			Status:            string(row.Status),
			CurrentDigest:     row.CurrentDigest,
			CurrentVersion:    row.CurrentVersion,
			NewDigest:         row.NewDigest,
			NewVersion:        row.NewVersion,
			PushedAt:          formatTimestamp(row.PushedAt),
			FirstSeen:         row.FirstSeen,
			EligibleAt:        formatTimestamp(row.EligibleAt),
			MinimumAgeSeconds: row.MinimumAgeSeconds,
			AgeSource:         ageSource(row, &application, inherited),
			Reason:            row.Reason,
		})
	}
	if !checkedAt.IsZero() {
		response.CheckedAt = formatTimestamp(&checkedAt)
	}
	c.JSON(http.StatusOK, response)
}

func ageSource(row *models.ApplicationImageUpdate, app *models.Application, inherited bool) string {
	if row.MinimumAgeFromLabel {
		return "label"
	}
	if _, ok := app.ReleaseAgeServiceOverrides[row.Service]; ok {
		return "service"
	}
	if inherited {
		return "global"
	}
	return "application"
}
