package routes

import (
	"context"
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

type generateImagePullWebhookResponse struct {
	Secret     string `json:"secret" required:"true" doc:"The secret that signs the webhook requests. Only returned once."`
	WebhookUrl string `json:"webhookUrl" required:"true" format:"uri"`
}

type imagePullWebhookOutput struct {
	Body generateImagePullWebhookResponse
}

var GenerateImagePullWebhookHandler = restapi.Handler(huma.Operation{
	OperationID: "generate-image-pull-webhook",
	Method:      http.MethodPost,
	Path:        "/applications/{id}/image-webhook",
	Summary:     "Generate the image pull webhook of an application",
	Description: "Returns a new secret for the webhook that pulls the images of the application, which is only shown once. " +
		"An existing secret stops working. Anyone with the secret can pull the images, so it also needs " +
		"the `application:pull_images` permission. " + requires(models.ResourceApplication, models.ActionUpdate),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusNotFound, http.StatusForbidden},
}, func(ctx context.Context, input *idInput) (*imagePullWebhookOutput, error) {
	id := input.ID

	app, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		return nil, internalError()
	}

	secret, err := auth.GenerateRandomString(32)
	if err != nil {
		return nil, internalError()
	}

	enc := crypto.EncryptedString(secret)
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", id).Update(ctx, "image_webhook_secret", &enc); err != nil {
		return nil, internalError()
	}

	utils.RecordAuditLog(restapi.GinContext(ctx), "enabled-image-webhook", "application", app.Id)

	webhookUrl := appUrl + "/api/v1/webhooks/images/" + app.Id
	sse.PublishUpdate(sse.TopicApplications.For(id))
	return &imagePullWebhookOutput{Body: generateImagePullWebhookResponse{
		Secret:     secret,
		WebhookUrl: webhookUrl,
	}}, nil
})

var RevokeImagePullWebhookHandler = restapi.Handler(huma.Operation{
	OperationID: "revoke-image-pull-webhook",
	Method:      http.MethodDelete,
	Path:        "/applications/{id}/image-webhook",
	Summary:     "Revoke the image pull webhook of an application",
	Description: "The secret of the webhook stops working. " + requires(models.ResourceApplication, models.ActionUpdate),
	Tags:        []string{tagApplications},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusNotFound, http.StatusForbidden},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	id := input.ID

	_, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		return nil, internalError()
	}

	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", id).Update(ctx, "image_webhook_secret", gorm.Expr("NULL")); err != nil {
		return nil, internalError()
	}

	utils.RecordAuditLog(restapi.GinContext(ctx), "disabled-image-webhook", "application", id)

	sse.PublishUpdate(sse.TopicApplications.For(id))
	return restapi.Message("webhook revoked"), nil
})
