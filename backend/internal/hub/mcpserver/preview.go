package mcpserver

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

// previewTTL is how long a preview can be deployed, so an old diff isn't
// approved by accident.
const previewTTL = 10 * time.Minute

var errPreviewMismatch = errors.New("the preview is expired, was already deployed, or the application changed since it was made; call preview_deploy again and show the user the new diff")

// previewKey signs the preview IDs. It isn't stored, previews end when the
// hub restarts.
var previewKey = rand.Text()

// now is replaced in tests.
var now = time.Now

// usedPreviews makes each preview deployable once, so a retrying client can't
// deploy again without the user seeing a new preview. The application state a
// preview covers doesn't change until a deploy succeeds.
var usedPreviews = previewSet{used: map[string]time.Time{}}

type previewSet struct {
	mu   sync.Mutex
	used map[string]time.Time
}

// use marks a preview as used until it expires. It reports false if it
// already was or has expired. Both are checked under the lock, so an entry is
// never dropped while its preview can still be deployed.
func (s *previewSet) use(previewID string, expiresAt time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := now()
	if current.After(expiresAt) {
		return false
	}
	if _, ok := s.used[previewID]; ok {
		return false
	}
	for id, expiry := range s.used {
		if current.After(expiry) {
			delete(s.used, id)
		}
	}
	s.used[previewID] = expiresAt
	return true
}

// previewID signs everything a preview shows, so a deploy is refused when any
// of it changed. It is not a security boundary, the token's scopes are: it
// keeps an agent from deploying something the user never saw.
func (c *caller) previewID(application *models.Application, issuedAt time.Time) string {
	issued := strconv.FormatInt(issuedAt.Unix(), 10)
	return issued + "." + base64.RawURLEncoding.EncodeToString(c.previewMAC(application, issued))
}

// validPreview checks a preview against the current state of the application
// and returns when it expires.
func (c *caller) validPreview(application *models.Application, previewID string) (time.Time, bool) {
	issued, _, ok := strings.Cut(previewID, ".")
	if !ok {
		return time.Time{}, false
	}
	issuedUnix, err := strconv.ParseInt(issued, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	expiresAt := time.Unix(issuedUnix, 0).Add(previewTTL)
	if now().After(expiresAt) {
		return time.Time{}, false
	}
	// The whole ID is compared, so each preview has one spelling and can't be
	// used again in another, like with a sign or a line break the decoders skip.
	if !hmac.Equal([]byte(previewID), []byte(c.previewID(application, time.Unix(issuedUnix, 0)))) {
		return time.Time{}, false
	}
	return expiresAt, true
}

func (c *caller) previewMAC(application *models.Application, issued string) []byte {
	composeHash := sha256.Sum256([]byte(application.ComposeFile.String()))
	rolledBackAt := ""
	if application.RolledBackAt != nil {
		rolledBackAt = application.RolledBackAt.UTC().Format(time.RFC3339Nano)
	}
	fields := []string{
		issued,
		c.user.Id,
		application.Id,
		// The name is the compose project on the agent.
		application.Name.String(),
		application.AgentId,
		strconv.FormatBool(application.DeleteOldImages),
		application.Commit,
		base64.RawURLEncoding.EncodeToString(composeHash[:]),
		deref(application.CurrentDeploymentId),
		rolledBackAt,
	}

	mac := hmac.New(sha256.New, []byte(previewKey))
	// Length-prefixed, as the name can contain any character, so no fields can
	// be shifted into each other.
	for _, field := range fields {
		mac.Write([]byte(strconv.Itoa(len(field)) + ":" + field))
	}
	return mac.Sum(nil)
}
