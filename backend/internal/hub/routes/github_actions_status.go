package routes

import (
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/githubactions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GitHubActionsTriggerStatusHandler returns the result to the originating workflow run.
func GitHubActionsTriggerStatusHandler(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	claims, ok := verifyGitHubActionsClaims(c)
	if !ok {
		return
	}
	if claims.Repository == "" || claims.RunId == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "trigger not found"})
		return
	}

	id := c.Param("id")
	result, err := githubactions.GetResult(c.Request.Context(), id, claims.Repository, claims.RunId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "trigger not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, result)
}
