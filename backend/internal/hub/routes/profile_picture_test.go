package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
)

func setUserClaimsWithPicture(t *testing.T, c *gin.Context, user *models.User, picture string) {
	t.Helper()

	token, err := auth.GenerateUserTokenWithPicture(user, picture)
	if err != nil {
		t.Fatalf("GenerateUserTokenWithPicture() error: %v", err)
	}

	claims, err := auth.ValidateUserToken(token)
	if err != nil {
		t.Fatalf("ValidateUserToken() error: %v", err)
	}

	auth.SetClaims(c, claims)
}

func useUnrestrictedPictureClient(t *testing.T) {
	t.Helper()
	original := profilePictureClient
	profilePictureClient = http.DefaultClient
	t.Cleanup(func() { profilePictureClient = original })
}

func TestProfilePictureHandler_NoClaims(t *testing.T) {
	setupTestDB(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile-picture", nil)

	ProfilePictureHandler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestProfilePictureHandler_NoPicture(t *testing.T) {
	setupTestDB(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile-picture", nil)
	setUserClaimsWithPicture(t, c, &models.User{Base: models.Base{Id: "user-1"}, Name: "Test User", Email: "test@example.com"}, "")

	ProfilePictureHandler(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestProfilePictureHandler_Success(t *testing.T) {
	setupTestDB(t)
	useUnrestrictedPictureClient(t)

	imageData := []byte("fake-png-bytes")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageData)
	}))
	t.Cleanup(upstream.Close)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile-picture", nil)
	setUserClaimsWithPicture(t, c, &models.User{Base: models.Base{Id: "user-1"}, Name: "Test User", Email: "test@example.com"}, upstream.URL+"/avatar.png")

	ProfilePictureHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("expected Content-Type image/png, got %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "private, max-age=900" {
		t.Errorf("expected private Cache-Control, got %q", got)
	}
	if w.Body.String() != string(imageData) {
		t.Errorf("expected image body to be passed through, got %q", w.Body.String())
	}
}

func TestProfilePictureHandler_UpstreamError(t *testing.T) {
	setupTestDB(t)
	useUnrestrictedPictureClient(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(upstream.Close)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile-picture", nil)
	setUserClaimsWithPicture(t, c, &models.User{Base: models.Base{Id: "user-1"}, Name: "Test User", Email: "test@example.com"}, upstream.URL+"/avatar.png")

	ProfilePictureHandler(c)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestProfilePictureHandler_NonImageContentType(t *testing.T) {
	setupTestDB(t)
	useUnrestrictedPictureClient(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not an image</html>"))
	}))
	t.Cleanup(upstream.Close)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile-picture", nil)
	setUserClaimsWithPicture(t, c, &models.User{Base: models.Base{Id: "user-1"}, Name: "Test User", Email: "test@example.com"}, upstream.URL+"/avatar.png")

	ProfilePictureHandler(c)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestProfilePictureURL(t *testing.T) {
	if got := profilePictureURL(""); got != "" {
		t.Errorf("expected empty string for empty picture, got %q", got)
	}
	if got := profilePictureURL("https://idp.example.com/avatar.png"); got != profilePicturePath {
		t.Errorf("expected %q, got %q", profilePicturePath, got)
	}
}
