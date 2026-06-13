package routes

import (
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type generateImagePullWebhookResponse struct {
	Secret     string `json:"secret"`
	WebhookUrl string `json:"webhookUrl"`
}

func GenerateImagePullWebhookHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	app, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	secret, err := auth.GenerateRandomString(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	enc := crypto.EncryptedString(secret)
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", id).Update(ctx, "image_webhook_secret", &enc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	utils.RecordAuditLog(c, "generated-image-pull-webhook", "application", app.Id)

	webhookUrl := appUrl + "/api/v1/webhooks/images/" + app.Id
	c.JSON(http.StatusOK, generateImagePullWebhookResponse{
		Secret:     secret,
		WebhookUrl: webhookUrl,
	})
	sse.PublishUpdate(ApplicationsPath)
}

func RevokeImagePullWebhookHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	_, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", id).Update(ctx, "image_webhook_secret", gorm.Expr("NULL")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	utils.RecordAuditLog(c, "revoked-image-pull-webhook", "application", id)

	c.JSON(http.StatusOK, gin.H{"message": "webhook revoked"})
	sse.PublishUpdate(ApplicationsPath)
}
