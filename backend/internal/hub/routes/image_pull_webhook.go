package routes

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/danielgtaylor/huma/v2"
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

type imagePullWebhookInput struct {
	ID              string `path:"id" doc:"The ID of the application"`
	GitHubEvent     string `header:"X-GitHub-Event" doc:"Set by GitHub: package events of published container images trigger a pull, ping is answered"`
	GitHubSignature string `header:"X-Hub-Signature-256" doc:"Set by GitHub: sha256= followed by the hex HMAC-SHA256 of the body, keyed with the webhook secret"`
	Authorization   string `header:"Authorization" doc:"Bearer followed by the webhook secret"`
	Token           string `query:"token" doc:"The webhook secret, for registries that can't set headers, like Docker Hub. Only read without an Authorization header."`
	RawBody         []byte
}

var imagePullWebhookRequestBody = &huma.RequestBody{
	Description: "The event of the registry. Harbor events only trigger a pull for pushed artifacts, " +
		"bodies without an event type always trigger one. May be empty.",
	Content: map[string]*huma.MediaType{
		"application/json": {Schema: &huma.Schema{Type: huma.TypeObject}},
	},
}

var ImagePullWebhookHandler = webhookHandler(huma.Operation{
	OperationID: "receive-image-webhook",
	Method:      http.MethodPost,
	Path:        "/webhooks/images/{id}",
	Summary:     "Receive an image webhook",
	Description: "Called by a container registry (GitHub Container Registry, Docker Hub, Harbor, or anything that can send a request) " +
		"after an image was pushed, to pull the images of an application and redeploy it. GitHub signs the body with the webhook secret, " +
		"other callers send it as `Authorization: Bearer <webhook secret>` or in the `token` query parameter.",
	Tags:          []string{tagWebhooks},
	DefaultStatus: http.StatusNoContent,
	Errors:        []int{http.StatusBadRequest, http.StatusUnauthorized},
}, imagePullWebhookRequestBody, func(ctx context.Context, input *imagePullWebhookInput) (*struct{}, error) {
	c := restapi.GinContext(ctx)

	app, err := gorm.G[models.Application](db.DB).Where("id = ?", input.ID).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, rejectWebhook(c, "unknown_id")
		}
		return nil, internalError()
	}

	if app.ImageWebhookSecret == nil {
		return nil, rejectWebhook(c, "webhook_disabled")
	}

	secret := app.ImageWebhookSecret.String()
	body := input.RawBody

	if event := input.GitHubEvent; event != "" {
		if !validateHMACSHA256(secret, body, strings.TrimPrefix(input.GitHubSignature, "sha256=")) {
			return nil, rejectWebhook(c, "invalid_signature")
		}
		if event == "ping" {
			return &struct{}{}, nil
		}
		if event != "package" {
			return nil, huma.Error400BadRequest("unsupported event type")
		}
		var payload githubPackagePayload
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, huma.Error400BadRequest("invalid webhook payload")
		}
		if !strings.EqualFold(payload.Package.PackageType, "CONTAINER") ||
			(!strings.EqualFold(payload.Action, "published") && !strings.EqualFold(payload.Action, "updated")) ||
			!isGitHubImageTagEvent(&payload) {
			return &struct{}{}, nil
		}
		applications.ScheduleImagePull(&app, models.ApplicationEventSourceImageWebhook)
		return &struct{}{}, nil
	}

	token := strings.TrimSpace(strings.TrimPrefix(input.Authorization, "Bearer "))
	if token == "" {
		token = input.Token
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
		return nil, rejectWebhook(c, "invalid_token")
	}

	// Docker Hub payloads carry a push_data field; all Docker Hub webhooks are push events.
	if isDockerHubPayload(body) {
		applications.ScheduleImagePull(&app, models.ApplicationEventSourceImageWebhook)
		return &struct{}{}, nil
	}

	// If the payload contains event_type (Harbor-style), only trigger on pushImage.
	if !isHarborPushEvent(body) {
		return &struct{}{}, nil
	}

	applications.ScheduleImagePull(&app, models.ApplicationEventSourceImageWebhook)
	return &struct{}{}, nil
})

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
// Harbor 2.x sends the event in "type" ("PUSH_ARTIFACT", or "harbor.artifact.pushed" in
// the CloudEvents format); Harbor 1.x used "event_type" ("pushImage"). Payloads with
// either field only trigger on a push; payloads with neither (simple generic webhooks)
// always trigger.
func isHarborPushEvent(body []byte) bool {
	if len(body) == 0 {
		return true
	}
	var payload struct {
		Type      string `json:"type"`
		EventType string `json:"event_type"`
	}
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
