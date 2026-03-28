package models

import (
	"testing"

	"gorm.io/gorm"
)

func TestBeforeCreate_GeneratesId(t *testing.T) {
	b := &Base{}
	if err := b.BeforeCreate(&gorm.DB{}); err != nil {
		t.Fatalf("BeforeCreate error: %v", err)
	}
	if b.Id == "" {
		t.Error("expected Id to be generated")
	}
	if b.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if b.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
}

func TestBeforeCreate_PreservesExistingId(t *testing.T) {
	b := &Base{Id: "custom-id"}
	if err := b.BeforeCreate(&gorm.DB{}); err != nil {
		t.Fatalf("BeforeCreate error: %v", err)
	}
	if b.Id != "custom-id" {
		t.Errorf("expected Id to remain %q, got %q", "custom-id", b.Id)
	}
}

func TestBeforeCreate_IdIsUUIDv7(t *testing.T) {
	b := &Base{}
	if err := b.BeforeCreate(&gorm.DB{}); err != nil {
		t.Fatalf("BeforeCreate error: %v", err)
	}
	if len(b.Id) != 36 {
		t.Errorf("expected UUID length 36, got %d (%q)", len(b.Id), b.Id)
	}
}

func TestBeforeCreate_UniqueIds(t *testing.T) {
	ids := make(map[string]bool)
	for range 100 {
		b := &Base{}
		if err := b.BeforeCreate(&gorm.DB{}); err != nil {
			t.Fatalf("BeforeCreate error: %v", err)
		}
		if ids[b.Id] {
			t.Fatalf("duplicate Id generated: %s", b.Id)
		}
		ids[b.Id] = true
	}
}
