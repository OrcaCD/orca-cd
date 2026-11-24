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
	"github.com/distribution/reference"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type githubPackagePayload struct {
	Action  string `json:"action"`
	Package struct {
		PackageType    string `json:"package_type"`
		PackageVersion *struct {
			// PackageURL is the pushed image, e.g. "ghcr.io/owner/name:tag".
			PackageURL        string `json:"package_url"`
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
	PushData *struct {
		Tag string `json:"tag"`
	} `json:"push_data"`
	Repository struct {
		RepoName string `json:"repo_name"`
	} `json:"repository"`
}

type harborPayload struct {
	Type      string `json:"type"`
	EventType string `json:"event_type"`
}

func ImagePullWebhookHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	app, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			rejectWebhook(c, "unknown_id")
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if app.ImageWebhookSecret == nil {
		rejectWebhook(c, "webhook_disabled")
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
			rejectWebhook(c, "invalid_signature")
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
		var image string
		if payload.Package.PackageVersion != nil {
			image = announcedImage(payload.Package.PackageVersion.PackageURL, "ghcr.io")
		}
		applications.ScheduleImagePull(&app, models.ApplicationEventSourceImageWebhook, image)
		c.AbortWithStatus(http.StatusNoContent)
		return
	}

	token := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	if token == "" {
		token = c.Query("token")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
		rejectWebhook(c, "invalid_token")
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodySize))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// Docker Hub payloads carry a push_data field; all Docker Hub webhooks are push events.
	if payload, ok := parseDockerHubPayload(body); ok {
		applications.ScheduleImagePull(&app, models.ApplicationEventSourceImageWebhook, dockerHubImage(payload))
		c.AbortWithStatus(http.StatusNoContent)
		return
	}

	// If the payload contains event_type (Harbor-style), only trigger on pushImage.
	if !isHarborPushEvent(body) {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}

	// The hub doesn't know the host of a Harbor registry, so it can't check
	// the image a Harbor payload names. The pull checks the release age of
	// every image instead.
	applications.ScheduleImagePull(&app, models.ApplicationEventSourceImageWebhook, "")
	c.AbortWithStatus(http.StatusNoContent)
}

func dockerHubImage(payload dockerHubPayload) string {
	if payload.Repository.RepoName == "" || payload.PushData.Tag == "" {
		return ""
	}
	return announcedImage(payload.Repository.RepoName+":"+payload.PushData.Tag, "docker.io")
}

// announcedImage returns image if it is in the registry of the provider that
// sent the webhook, or empty otherwise. An announced image skips the release
// age, so a payload must not name the image of another registry.
func announcedImage(image, registry string) string {
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil || reference.Domain(named) != registry {
		return ""
	}
	return image
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

// parseDockerHubPayload parses body when it contains a Docker Hub push_data field.
// Docker Hub does not support custom auth headers, so authentication is handled via
// the ?token query parameter before this function is called.
func parseDockerHubPayload(body []byte) (dockerHubPayload, bool) {
	var payload dockerHubPayload
	if len(body) == 0 {
		return payload, false
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return payload, false
	}
	return payload, payload.PushData != nil
}

// isHarborPushEvent returns true when the request should trigger an image pull.
// Harbor 2.x sends the event in "type" ("PUSH_ARTIFACT", or "harbor.artifact.pushed" in
// the CloudEvents format); Harbor 1.x used "event_type" ("pushImage"). Payloads with
// either field only trigger on a push; payloads with neither (simple generic webhooks)
// always trigger.
func isHarborPushEvent(body []byte) bool {
	if len(body) == 0 {
		return true
	}
	var payload harborPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return true
	}
	switch {
	case payload.Type != "":
		return payload.Type == "PUSH_ARTIFACT" || payload.Type == "harbor.artifact.pushed"
	case payload.EventType != "":
		return payload.EventType == "pushImage"
	default:
		return true
	}
}
