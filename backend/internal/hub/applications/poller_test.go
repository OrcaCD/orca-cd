package applications

import (
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

func TestIsDue(t *testing.T) {
	now := time.Now()
	interval := time.Duration(60)

	repoWithInterval := func(lastSyncedAt *time.Time) models.Repository {
		return models.Repository{
			PollingInterval: &interval,
			LastSyncedAt:    lastSyncedAt,
		}
	}

	t.Run("nil interval is never due", func(t *testing.T) {
		repo := models.Repository{PollingInterval: nil}
		if isDue(&repo, now) {
			t.Error("expected isDue=false for nil interval")
		}
	})

	t.Run("nil lastSyncedAt is always due", func(t *testing.T) {
		repo := repoWithInterval(nil)
		if !isDue(&repo, now) {
			t.Error("expected isDue=true when never synced")
		}
	})

	t.Run("just synced is not due", func(t *testing.T) {
		recent := now.Add(-10 * time.Second)
		repo := repoWithInterval(&recent)
		if isDue(&repo, now) {
			t.Error("expected isDue=false when synced 10s ago with 60s interval")
		}
	})

	t.Run("exactly at interval boundary is due", func(t *testing.T) {
		boundary := now.Add(-(interval * time.Second))
		repo := repoWithInterval(&boundary)
		if !isDue(&repo, now) {
			t.Error("expected isDue=true at exact interval boundary")
		}
	})

	t.Run("overdue is due", func(t *testing.T) {
		old := now.Add(-(2 * interval * time.Second))
		repo := repoWithInterval(&old)
		if !isDue(&repo, now) {
			t.Error("expected isDue=true when overdue")
		}
	})
}
