package routes

import (
	"errors"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/healthhistory"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var applicationHealthLog = logger.New("application-health", false)

// GetApplicationHealthHandler returns the availability statistics of an
// application for the requested range (24h, 7d or 30d; 24h by default).
func GetApplicationHealthHandler(c *gin.Context) {
	ctx := c.Request.Context()
	applicationID := c.Param("id")

	r, ok := healthhistory.ParseRange(c.DefaultQuery("range", string(healthhistory.Range24h)))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid range: must be one of 24h, 7d, 30d"})
		return
	}

	if _, err := gorm.G[models.Application](db.DB).
		Select("id").
		Where("id = ?", applicationID).
		First(ctx); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
			return
		}
		applicationHealthLog.Error().Err(err).Str("applicationId", applicationID).
			Msg("failed to load application for health statistics")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	stats, err := healthhistory.GetStats(ctx, applicationID, r, time.Now())
	if err != nil {
		applicationHealthLog.Error().Err(err).Str("applicationId", applicationID).
			Msg("failed to compute application health statistics")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, stats)
}
