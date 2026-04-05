package routes

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type createOIDCProviderRequest struct {
	Name         string `json:"name" binding:"required,min=1,max=100"`
	IssuerURL    string `json:"issuerUrl" binding:"required,http_url"`
	ClientId     string `json:"clientId" binding:"required"`
	ClientSecret string `json:"clientSecret" binding:"required"`
	Scopes       string `json:"scopes"`
	Enabled      *bool  `json:"enabled"`
}

type updateOIDCProviderRequest struct {
	Name         string  `json:"name" binding:"required,min=1,max=100"`
	IssuerURL    string  `json:"issuerUrl" binding:"required,http_url"`
	ClientId     string  `json:"clientId" binding:"required"`
	ClientSecret *string `json:"clientSecret"`
	Scopes       string  `json:"scopes"`
	Enabled      *bool   `json:"enabled"`
}

type oidcProviderResponse struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	IssuerURL   string `json:"issuerUrl"`
	ClientId    string `json:"clientId"`
	Scopes      string `json:"scopes"`
	Enabled     bool   `json:"enabled"`
	CallbackURL string `json:"callbackUrl"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

func toOIDCProviderResponse(p *models.OIDCProvider) oidcProviderResponse {
	return oidcProviderResponse{
		Id:          p.Id,
		Name:        p.Name,
		IssuerURL:   p.IssuerURL,
		ClientId:    p.ClientId,
		Scopes:      p.Scopes,
		Enabled:     p.Enabled,
		CallbackURL: fmt.Sprintf("%s/api/v1/auth/oidc/%s/callback", OIDCAppURL, p.Id),
		CreatedAt:   p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   p.UpdatedAt.Format(time.RFC3339),
	}
}

func AdminListOIDCProvidersHandler(c *gin.Context) {
	providers, err := gorm.G[models.OIDCProvider](db.DB).Find(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	result := make([]oidcProviderResponse, 0, len(providers))
	for _, p := range providers {
		result = append(result, toOIDCProviderResponse(&p))
	}

	c.JSON(http.StatusOK, result)
}

func AdminGetOIDCProviderHandler(c *gin.Context) {
	id := c.Param("id")

	provider, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ?", id).First(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	c.JSON(http.StatusOK, toOIDCProviderResponse(&provider))
}

func AdminCreateOIDCProviderHandler(c *gin.Context) {
	var req createOIDCProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name, issuerUrl, clientId, and clientSecret are required and must be valid"})
		return
	}

	// Validate issuer by attempting OIDC discovery
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if _, err := gooidc.NewProvider(ctx, req.IssuerURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to discover OIDC provider at the given issuer URL"})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	provider := models.OIDCProvider{
		Name:         req.Name,
		IssuerURL:    req.IssuerURL,
		ClientId:     req.ClientId,
		ClientSecret: crypto.EncryptedString(req.ClientSecret),
		Scopes:       req.Scopes,
		Enabled:      enabled,
	}

	if err := gorm.G[models.OIDCProvider](db.DB).Create(c.Request.Context(), &provider); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, toOIDCProviderResponse(&provider))
}

func AdminUpdateOIDCProviderHandler(c *gin.Context) {
	id := c.Param("id")

	var req updateOIDCProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name, issuerUrl, and clientId are required and must be valid"})
		return
	}

	existing, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ?", id).First(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	// Validate issuer if it changed
	if req.IssuerURL != existing.IssuerURL {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		if _, err := gooidc.NewProvider(ctx, req.IssuerURL); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to discover OIDC provider at the given issuer URL"})
			return
		}
	}

	existing.Name = req.Name
	existing.IssuerURL = req.IssuerURL
	existing.ClientId = req.ClientId
	existing.Scopes = req.Scopes
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.ClientSecret != nil && *req.ClientSecret != "" {
		existing.ClientSecret = crypto.EncryptedString(*req.ClientSecret)
	}

	if err := db.DB.WithContext(c.Request.Context()).Save(&existing).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, toOIDCProviderResponse(&existing))
}

func AdminDeleteOIDCProviderHandler(c *gin.Context) {
	id := c.Param("id")

	result := db.DB.WithContext(c.Request.Context()).Where("id = ?", id).Delete(&models.OIDCProvider{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "provider deleted"})
}
