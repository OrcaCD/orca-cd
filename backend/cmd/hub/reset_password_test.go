package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupCommandTestDB(t *testing.T) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	testDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	if err := testDB.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}

	sqlDB, err := testDB.DB()
	if err != nil {
		t.Fatalf("failed to access sql db: %v", err)
	}

	db.DB = testDB
	t.Cleanup(func() {
		_ = sqlDB.Close()
		db.DB = nil
	})
}

func createLocalTestUser(t *testing.T, email string, password string) models.User {
	t.Helper()

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	user := models.User{
		Name:                   "Test User",
		Email:                  email,
		PasswordHash:           &hash,
		Role:                   models.UserRoleUser,
		PasswordChangeRequired: false,
	}

	if err := gorm.G[models.User](db.DB).Create(t.Context(), &user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	return user
}

func TestNormalizeResetPasswordTarget(t *testing.T) {
	t.Parallel()

	_, err := normalizeResetPasswordTarget(resetPasswordTarget{})
	if !errors.Is(err, errMissingUserIdentifier) {
		t.Fatalf("expected errMissingUserIdentifier, got %v", err)
	}

	_, err = normalizeResetPasswordTarget(resetPasswordTarget{Email: "user@example.com", ID: "abc"})
	if !errors.Is(err, errTooManyIdentifiers) {
		t.Fatalf("expected errTooManyIdentifiers, got %v", err)
	}

	normalized, err := normalizeResetPasswordTarget(resetPasswordTarget{Email: "  USER@Example.com  "})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if normalized.Email != "user@example.com" {
		t.Fatalf("expected normalized email, got %q", normalized.Email)
	}
}

func TestResetUserPasswordByEmail(t *testing.T) {
	setupCommandTestDB(t)
	user := createLocalTestUser(t, "user@example.com", "old-password")

	updatedUser, generatedPassword, err := resetUserPassword(t.Context(), resetPasswordTarget{Email: "USER@EXAMPLE.COM"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if generatedPassword == "" {
		t.Fatal("expected generated password")
	}

	if updatedUser.Id != user.Id {
		t.Fatalf("expected user id %q, got %q", user.Id, updatedUser.Id)
	}

	storedUser, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load updated user: %v", err)
	}

	if storedUser.PasswordHash == nil || !auth.CheckPassword(generatedPassword, *storedUser.PasswordHash) {
		t.Fatal("expected password hash to match generated password")
	}

	if !storedUser.PasswordChangeRequired {
		t.Fatal("expected passwordChangeRequired=true after reset")
	}
}

func TestResetUserPasswordRejectsManagedUser(t *testing.T) {
	setupCommandTestDB(t)

	managedUser := models.User{
		Name:                   "OIDC User",
		Email:                  "oidc@example.com",
		PasswordHash:           nil,
		Role:                   models.UserRoleUser,
		PasswordChangeRequired: false,
	}

	if err := gorm.G[models.User](db.DB).Create(t.Context(), &managedUser); err != nil {
		t.Fatalf("failed to create managed user: %v", err)
	}

	_, _, err := resetUserPassword(t.Context(), resetPasswordTarget{Email: managedUser.Email})
	if !errors.Is(err, errManagedUser) {
		t.Fatalf("expected errManagedUser, got %v", err)
	}
}
