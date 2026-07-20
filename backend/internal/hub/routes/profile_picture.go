package routes

import (
	"io"
	"net/http"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
	"github.com/gin-gonic/gin"
)

// profilePicturePath is the self-origin path the SPA loads the avatar from.
// The hub proxies the request to the OIDC provider so the strict CSP/COEP
// headers don't have to allow external image origins.
const profilePicturePath = "/api/v1/auth/profile-picture"

const maxProfilePictureBytes = 5 << 20 // 5 MiB

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
		c.JSON(http.StatusBadGateway, gin.H{"error": "invalid profile picture URL"})
		return
	}

	resp, err := profilePictureClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to reach profile picture provider"})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": "profile picture provider returned an error"})
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		c.JSON(http.StatusBadGateway, gin.H{"error": "profile picture provider did not return an image"})
		return
	}

	// Reject on the declared size before reading anything, so an upstream
	// that's honest about Content-Length never gets buffered into memory.
	if resp.ContentLength > maxProfilePictureBytes {
		c.JSON(http.StatusBadGateway, gin.H{"error": "profile picture is too large"})
		return
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxProfilePictureBytes+1))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read profile picture"})
		return
	}
	if len(data) > maxProfilePictureBytes {
		c.JSON(http.StatusBadGateway, gin.H{"error": "profile picture is too large"})
		return
	}

	c.Header("Cache-Control", "private, max-age=900")
	c.Data(http.StatusOK, contentType, data)
}
