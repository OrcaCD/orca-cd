package routes

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/releaseage"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
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

// nullableSeconds is a number of seconds that may be null, telling a null
// apart from a missing field.
type nullableSeconds struct {
	Set   bool
	Value *int64
}

func (n *nullableSeconds) UnmarshalJSON(data []byte) error {
	n.Set = true
	n.Value = nil
	if string(data) == "null" {
		return nil
	}
	var seconds int64
	if err := json.Unmarshal(data, &seconds); err != nil {
		return err
	}
	n.Value = &seconds
	return nil
}

// checkMinimumSeconds answers with 400 and returns false if seconds is not a
// valid minimum release age, which must not be below the global one.
func checkMinimumSeconds(c *gin.Context, seconds *int64, global models.Settings) bool {
	if seconds == nil {
		return true
	}
	if !validMinimumSeconds(*seconds) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "minimum release age must be between 0 and 365 days"})
		return false
	}
	if *seconds < global.MinimumReleaseAgeSeconds {
		c.JSON(http.StatusBadRequest, gin.H{"error": "minimum release age must not be below the global minimum release age"})
		return false
	}
	return true
}

// loadGlobalReleaseAge returns the global settings, or answers with 500.
func loadGlobalReleaseAge(c *gin.Context) (models.Settings, bool) {
	global, err := releaseage.Global(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return global, false
	}
	return global, true
}

type updateReleaseAgeRequest struct {
	MinimumSeconds    nullableSeconds `json:"minimumSeconds"`
	AllowedRegistries []string        `json:"allowedRegistries" binding:"required"`
}

// UpdateReleaseAgeSettingsHandler configures the minimum release age of the
// images an application is updated to. A null minimumSeconds uses the global
// one, which is also the least minimum release age.
func UpdateReleaseAgeSettingsHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var req updateReleaseAgeRequest
	if err := c.ShouldBindJSON(&req); err != nil || !req.MinimumSeconds.Set {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	global, ok := loadGlobalReleaseAge(c)
	if !ok || !checkMinimumSeconds(c, req.MinimumSeconds.Value, global) {
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
			MinimumReleaseAgeSeconds:    req.MinimumSeconds.Value,
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

const (
	maxServiceReleaseAgeOverrides = 100
	maxServiceNameLength          = 255
)

type updateServiceReleaseAgeRequest struct {
	MinimumSeconds nullableSeconds `json:"minimumSeconds"`
}

// serviceOverridesMu serializes changes to the service overrides, which are
// read, changed and written back as a whole.
var serviceOverridesMu sync.Mutex

// UpdateServiceReleaseAgeHandler overrides the minimum release age of one
// compose service. A null minimumSeconds removes the override.
func UpdateServiceReleaseAgeHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")
	service := c.Param("service")

	var req updateServiceReleaseAgeRequest
	if err := c.ShouldBindJSON(&req); err != nil || !req.MinimumSeconds.Set || service == "" || len(service) > maxServiceNameLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	global, ok := loadGlobalReleaseAge(c)
	if !ok || !checkMinimumSeconds(c, req.MinimumSeconds.Value, global) {
		return
	}

	serviceOverridesMu.Lock()
	defer serviceOverridesMu.Unlock()

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
	if req.MinimumSeconds.Value == nil {
		delete(overrides, service)
	} else {
		if _, exists := overrides[service]; !exists && len(overrides) >= maxServiceReleaseAgeOverrides {
			c.JSON(http.StatusBadRequest, gin.H{"error": "the minimum release age can be overridden for at most 100 services"})
			return
		}
		overrides[service] = *req.MinimumSeconds.Value
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
	sse.PublishUpdate(sse.TopicApplications.For(id))
	sendAgentSettings(c, application.AgentId)
}

// GetReleaseAgeSettingsHandler returns the global release age settings.
func GetReleaseAgeSettingsHandler(c *gin.Context) {
	settings, ok := loadGlobalReleaseAge(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, globalReleaseAgeSettings{
		MinimumSeconds:    settings.MinimumReleaseAgeSeconds,
		AllowedRegistries: nonNil(settings.ReleaseAgeAllowedRegistries),
	})
}

type updateGlobalReleaseAgeRequest struct {
	MinimumSeconds    *int64   `json:"minimumSeconds" binding:"required"`
	AllowedRegistries []string `json:"allowedRegistries" binding:"required"`
}

// AdminUpdateReleaseAgeSettingsHandler changes the global release age settings.
// The minimum release age is the default of applications without their own,
// and the least one of every image that is not from an allowed registry.
func AdminUpdateReleaseAgeSettingsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	var req updateGlobalReleaseAgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if !validMinimumSeconds(*req.MinimumSeconds) {
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
		MinimumReleaseAgeSeconds:    *req.MinimumSeconds,
		ReleaseAgeAllowedRegistries: registries,
	}
	if err := gorm.G[models.Settings](db.DB, clause.OnConflict{UpdateAll: true}).Create(ctx, &settings); err != nil {
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
		releaseAgeLog.Error().Err(err).Msg("failed to list the agents to send the release age settings to")
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
	if row.MinimumAgeEnforced {
		return "global"
	}
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
