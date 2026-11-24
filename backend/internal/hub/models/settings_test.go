package models

import "testing"

// The migrations create these tables, so the names must not change.
func TestReleaseAgeTableNames(t *testing.T) {
	if got := (Settings{}).TableName(); got != "settings" {
		t.Errorf("Settings table = %q", got)
	}
	if got := (ApplicationImageUpdate{}).TableName(); got != "application_image_updates" {
		t.Errorf("ApplicationImageUpdate table = %q", got)
	}
}
