package applications

import (
	"testing"
)

func TestGetAllApplicationsForRepo(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)
	agent := seedAgent(t)
	seedApp(t, repo.Id, agent.Id, "compose: v1")
	seedApp(t, repo.Id, agent.Id, "compose: v2")

	apps, err := GetAllApplicationsForRepo(t.Context(), &repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps for repo, got %d", len(apps))
	}
	for _, app := range apps {
		if app.RepositoryId != repo.Id {
			t.Errorf("got app with repository_id %q, want %q", app.RepositoryId, repo.Id)
		}
	}
}

func TestGetAllApplicationsForRepo_Empty(t *testing.T) {
	setupTestDB(t)
	repo := seedRepo(t)

	apps, err := GetAllApplicationsForRepo(t.Context(), &repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("expected 0 apps, got %d", len(apps))
	}
}
