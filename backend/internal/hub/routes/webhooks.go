package routes

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// maxWebhookBodySize caps the request body to guard against memory exhaustion from oversized payloads.
const maxWebhookBodySize = 10 * 1024 * 1024 // 10 MB

func WebhookHandler(c *gin.Context) {
	id := c.Param("id")

	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", id).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if repo.SyncType != models.SyncTypeWebhook {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repository is not configured for webhook sync"})
		return
	}

	if repo.WebhookSecret == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodySize))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	secret := repo.WebhookSecret.String()

	if !validateSignature(c, repo.Provider, secret, body) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook signature"})
		return
	}

	if !isPushEvent(c, repo.Provider) {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}

	now := time.Now()
	if _, err := gorm.G[models.Repository](db.DB).Where("id = ?", id).Updates(c.Request.Context(), models.Repository{
		SyncStatus:   models.SyncStatusSuccess,
		LastSyncedAt: &now,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// Todo trigger application sync

	c.AbortWithStatus(http.StatusNoContent)
}

func validateSignature(c *gin.Context, provider models.RepositoryProvider, secret string, body []byte) bool {
	switch provider {
	case models.GitHub:
		return validateHMACSHA256(secret, body, strings.TrimPrefix(c.GetHeader("X-Hub-Signature-256"), "sha256="))
	case models.Gitea:
		return validateHMACSHA256(secret, body, c.GetHeader("X-Gitea-Signature"))
	case models.GitLab:
		token := c.GetHeader("X-Gitlab-Token")
		return subtle.ConstantTimeCompare([]byte(token), []byte(secret)) == 1
	default:
		return false
	}
}

func validateHMACSHA256(secret string, body []byte, hexSig string) bool {
	if hexSig == "" {
		return false
	}
	sig, err := hex.DecodeString(hexSig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), sig)
}

func isPushEvent(c *gin.Context, provider models.RepositoryProvider) bool {
	switch provider {
	case models.GitHub:
		return c.GetHeader("X-GitHub-Event") == "push"
	case models.Gitea:
		return c.GetHeader("X-Gitea-Event") == "push"
	case models.GitLab:
		return c.GetHeader("X-Gitlab-Event") == "Push Hook"
	default:
		return false
	}
}
