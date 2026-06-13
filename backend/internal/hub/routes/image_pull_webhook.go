package routes

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type githubPackagePayload struct {
	Action  string `json:"action"`
	Package struct {
		PackageType string `json:"package_type"`
	} `json:"package"`
}

func ImagePullWebhookHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	app, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// Return 404 rather than 401 when no webhook is configured, to avoid leaking app existence.
	if app.ImageWebhookSecret == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	secret := app.ImageWebhookSecret.String()

	if c.GetHeader("X-GitHub-Event") == "package" {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodySize))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
		if !validateHMACSHA256(secret, body, strings.TrimPrefix(c.GetHeader("X-Hub-Signature-256"), "sha256=")) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook signature"})
			return
		}
		var payload githubPackagePayload
		if err := json.Unmarshal(body, &payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook payload"})
			return
		}
		if !strings.EqualFold(payload.Package.PackageType, "CONTAINER") ||
			(!strings.EqualFold(payload.Action, "published") && !strings.EqualFold(payload.Action, "updated")) {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		applications.TriggerImagePull(&app)
		c.AbortWithStatus(http.StatusNoContent)
		return
	}

	token := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	if subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodySize))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// If the payload contains event_type (Harbor-style), only trigger on pushImage.
	if !isHarborPushEvent(body) {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}

	applications.TriggerImagePull(&app)
	c.AbortWithStatus(http.StatusNoContent)
}

// isHarborPushEvent returns true when the request should trigger an image pull.
// Payloads with a Harbor-style event_type field only trigger on "pushImage";
// payloads without event_type (simple generic webhooks) always trigger.
func isHarborPushEvent(body []byte) bool {
	if len(body) == 0 {
		return true
	}
	var payload struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.EventType == "" {
		return true
	}
	return payload.EventType == "pushImage"
}
