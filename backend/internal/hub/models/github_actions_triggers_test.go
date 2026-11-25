package models

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestGitHubActionsTriggerModelsMatchMigrationTableNames(t *testing.T) {
	for _, tt := range []struct {
		model any
		table string
	}{
		{&GitHubActionsTrigger{}, "github_actions_triggers"},
		{&GitHubActionsTriggerSkip{}, "github_actions_trigger_skips"},
	} {
		parsed, err := schema.Parse(tt.model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse trigger schema: %v", err)
		}
		if parsed.Table != tt.table {
			t.Errorf("table for %T = %q, want %q", tt.model, parsed.Table, tt.table)
		}
	}
}
