package mcpserver

import (
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

func previewApplication() *models.Application {
	deploymentId := "deployment-1"
	return &models.Application{
		Id:                  "app-1",
		Name:                crypto.EncryptedString("web"),
		AgentId:             "agent-1",
		Commit:              "new-commit",
		ComposeFile:         crypto.EncryptedString("services:\n  web:\n    image: nginx:1.27\n"),
		CurrentDeploymentId: &deploymentId,
	}
}

// freezeNow sets the clock of the package and returns a pointer to move it.
func freezeNow(t *testing.T) *time.Time {
	t.Helper()
	current := time.Unix(1_800_000_000, 0)
	now = func() time.Time { return current }
	t.Cleanup(func() { now = time.Now })
	return &current
}

func TestValidPreview_AcceptsThePreviewUntilItExpires(t *testing.T) {
	current := freezeNow(t)
	c := &caller{user: testUser()}
	app := previewApplication()
	previewID := c.previewID(app, *current)

	expiresAt, ok := c.validPreview(app, previewID)
	if !ok || !expiresAt.Equal(current.Add(previewTTL)) {
		t.Fatalf("validPreview() = %v, %v, want valid until %v", expiresAt, ok, current.Add(previewTTL))
	}

	*current = current.Add(previewTTL + time.Second)
	if _, ok := c.validPreview(app, previewID); ok {
		t.Error("expected an expired preview to be invalid")
	}
}

// A preview has one spelling, or the used previews could be bypassed by
// spelling a used one differently.
func TestValidPreview_RejectsOtherSpellings(t *testing.T) {
	current := freezeNow(t)
	c := &caller{user: testUser()}
	app := previewApplication()
	previewID := c.previewID(app, *current)

	for name, spelling := range map[string]string{
		"line feed":       previewID + "\n",
		"carriage return": previewID + "\r",
		"inner line feed": previewID[:len(previewID)-2] + "\n" + previewID[len(previewID)-2:],
		"padding":         previewID + "=",
		"plus sign":       "+" + previewID,
		"leading zero":    "0" + previewID,
		"no signature":    previewID[:len(previewID)-43],
		"empty":           "",
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := c.validPreview(app, spelling); ok {
				t.Errorf("expected %q to be invalid", spelling)
			}
		})
	}
}

func TestValidPreview_RejectsChangedApplications(t *testing.T) {
	current := freezeNow(t)
	c := &caller{user: testUser()}
	previewID := c.previewID(previewApplication(), *current)

	rolledBackAt := current.Add(-time.Hour)
	otherDeployment := "deployment-2"
	for name, change := range map[string]func(*models.Application){
		"id":                 func(a *models.Application) { a.Id = "app-2" },
		"name":               func(a *models.Application) { a.Name = "api" },
		"agent":              func(a *models.Application) { a.AgentId = "agent-2" },
		"delete old images":  func(a *models.Application) { a.DeleteOldImages = true },
		"commit":             func(a *models.Application) { a.Commit = "newer-commit" },
		"compose file":       func(a *models.Application) { a.ComposeFile = "services: {}\n" },
		"running deployment": func(a *models.Application) { a.CurrentDeploymentId = &otherDeployment },
		"rolled back":        func(a *models.Application) { a.RolledBackAt = &rolledBackAt },
	} {
		t.Run(name, func(t *testing.T) {
			app := previewApplication()
			change(app)
			if _, ok := c.validPreview(app, previewID); ok {
				t.Error("expected the preview to be invalid")
			}
		})
	}

	t.Run("other user", func(t *testing.T) {
		other := &caller{user: &models.User{Id: "user-2"}}
		if _, ok := other.validPreview(previewApplication(), previewID); ok {
			t.Error("expected the preview of another user to be invalid")
		}
	})
}

func TestPreviewSet_ExpiredPreviewsCantBeUsed(t *testing.T) {
	start := time.Now()
	current := start
	now = func() time.Time { return current }
	t.Cleanup(func() { now = time.Now })

	set := previewSet{used: map[string]time.Time{}}
	expiresAt := start.Add(previewTTL)
	if !set.use("preview", expiresAt) {
		t.Fatal("expected the first use to succeed")
	}
	if set.use("preview", expiresAt) {
		t.Error("expected a second use to fail")
	}

	// Past its expiry, a used preview is dropped from the set, so the set must
	// reject it for being expired instead.
	current = expiresAt.Add(time.Millisecond)
	if set.use("preview", expiresAt) {
		t.Error("expected an expired preview to fail")
	}
	if set.use("other", current.Add(previewTTL)); len(set.used) != 1 {
		t.Errorf("expected expired entries to be dropped, got %v", set.used)
	}
}
