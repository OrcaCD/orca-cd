package routes

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
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
		PackageType    string `json:"package_type"`
		PackageVersion *struct {
			ContainerMetadata *struct {
				Tag *struct {
					Name string `json:"name"`
				} `json:"tag"`
			} `json:"container_metadata"`
		} `json:"package_version"`
	} `json:"package"`
}

// signatureTagPattern matches tags registries derive from a manifest digest to attach
// artifacts to an image: cosign signatures/attestations/SBOMs (sha256-<hex>.sig) and
// the OCI referrers fallback tag (sha256-<hex>).
var signatureTagPattern = regexp.MustCompile(`^sha256-[a-f0-9]{64}(\..+)?$`)

type dockerHubPayload struct {
	PushData *json.RawMessage `json:"push_data"`
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

	if event := c.GetHeader("X-GitHub-Event"); event != "" {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodySize))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
		if !validateHMACSHA256(secret, body, strings.TrimPrefix(c.GetHeader("X-Hub-Signature-256"), "sha256=")) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook signature"})
			return
		}
		if event == "ping" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		if event != "package" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported event type"})
			return
		}
		var payload githubPackagePayload
		if err := json.Unmarshal(body, &payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook payload"})
			return
		}
		if !strings.EqualFold(payload.Package.PackageType, "CONTAINER") ||
			(!strings.EqualFold(payload.Action, "published") && !strings.EqualFold(payload.Action, "updated")) ||
			!isGitHubImageTagEvent(&payload) {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		applications.ScheduleImagePull(&app, models.ApplicationEventSourceImageWebhook)
		c.AbortWithStatus(http.StatusNoContent)
		return
	}

	token := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	if token == "" {
		token = c.Query("token")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodySize))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// Docker Hub payloads carry a push_data field; all Docker Hub webhooks are push events.
	if isDockerHubPayload(body) {
		applications.ScheduleImagePull(&app, models.ApplicationEventSourceImageWebhook)
		c.AbortWithStatus(http.StatusNoContent)
		return
	}

	// If the payload contains event_type (Harbor-style), only trigger on pushImage.
	if !isHarborPushEvent(body) {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}

	applications.ScheduleImagePull(&app, models.ApplicationEventSourceImageWebhook)
	c.AbortWithStatus(http.StatusNoContent)
}

// isGitHubImageTagEvent reports whether a GHCR package event moved a tag an application
// can reference. One push of a multi-arch image fans out into a delivery per manifest:
// the untagged per-platform images and attestations, the tagged manifest list, and
// possibly signatures pushed afterwards. Only the tagged manifest changes what a compose
// file resolves to, so the other deliveries would only cause redundant pulls.
// Payloads without container metadata are let through.
func isGitHubImageTagEvent(payload *githubPackagePayload) bool {
	version := payload.Package.PackageVersion
	if version == nil || version.ContainerMetadata == nil || version.ContainerMetadata.Tag == nil {
		return true
	}
	tag := strings.TrimSpace(version.ContainerMetadata.Tag.Name)
	return tag != "" && !signatureTagPattern.MatchString(tag)
}

// isDockerHubPayload returns true when body contains a Docker Hub push_data field.
// Docker Hub does not support custom auth headers, so authentication is handled via
// the ?token query parameter before this function is called.
func isDockerHubPayload(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var payload dockerHubPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return payload.PushData != nil
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
