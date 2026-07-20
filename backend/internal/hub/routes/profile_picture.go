package routes

import (
	"io"
	"net/http"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/gin-gonic/gin"
)

// profilePicturePath is the self-origin path the SPA loads the avatar from.
// The hub proxies the request to the OIDC provider so the strict CSP/COEP
// headers don't have to allow external image origins.
const profilePicturePath = "/api/v1/auth/profile-picture"

const maxProfilePictureBytes = 5 << 20 // 5 MiB

var profilePictureLog = logger.New("profile-picture", false)

// profilePictureClient is the HTTP client used to fetch the picture from the
// OIDC provider. Replaced with an unrestricted client in tests.
var profilePictureClient = httpclient.Default

func profilePictureURL(picture string) string {
	if picture == "" {
		return ""
	}
	return profilePicturePath
}

func ProfilePictureHandler(c *gin.Context) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return
	}

	if claims.Picture == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no profile picture available"})
		return
	}

	req, err := httpclient.NewRequest(c.Request.Context(), http.MethodGet, claims.Picture, nil)
	if err != nil {
		profilePictureLog.Error().Err(err).Str("subject", claims.Subject).
			Msg("failed to build profile picture request")
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to load profile picture"})
		return
	}

	resp, err := profilePictureClient.Do(req)
	if err != nil {
		profilePictureLog.Error().Err(err).Str("subject", claims.Subject).
			Msg("failed to fetch profile picture from upstream")
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to load profile picture"})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	contentType := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		profilePictureLog.Warn().Str("subject", claims.Subject).
			Int("statusCode", resp.StatusCode).Str("contentType", contentType).
			Msg("upstream returned a non-image profile picture response")
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to load profile picture"})
		return
	}

	// Reject on the declared size before reading anything, so an upstream
	// that's honest about Content-Length never gets buffered into memory.
	if resp.ContentLength > maxProfilePictureBytes {
		profilePictureLog.Warn().Str("subject", claims.Subject).
			Int64("contentLength", resp.ContentLength).
			Msg("upstream profile picture exceeds size limit")
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to load profile picture"})
		return
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxProfilePictureBytes+1))
	if err != nil {
		profilePictureLog.Error().Err(err).Str("subject", claims.Subject).
			Msg("failed to read profile picture body")
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to load profile picture"})
		return
	}
	if len(data) > maxProfilePictureBytes {
		profilePictureLog.Warn().Str("subject", claims.Subject).
			Msg("profile picture body exceeds size limit")
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to load profile picture"})
		return
	}

	c.Header("Cache-Control", "private, max-age=900")
	c.Data(http.StatusOK, contentType, data)
}
