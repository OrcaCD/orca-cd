package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/passkeys"
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

	if err := testDB.AutoMigrate(&models.User{}, &models.Passkey{}, &models.PasskeyEnrollment{}); err != nil {
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

func createLocalTestUser(t *testing.T, email string) models.User {
	t.Helper()

	user := models.User{Name: "Test User", Email: email, LocalAuthEnabled: true}

	if err := gorm.G[models.User](db.DB).Create(t.Context(), &user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	return user
}

func TestNormalizeResetPasskeysTarget(t *testing.T) {
	t.Parallel()

	_, err := normalizeResetPasskeysTarget(resetPasskeysTarget{})
	if !errors.Is(err, errMissingUserIdentifier) {
		t.Fatalf("expected errMissingUserIdentifier, got %v", err)
	}

	_, err = normalizeResetPasskeysTarget(resetPasskeysTarget{Email: "user@example.com", ID: "abc"})
	if !errors.Is(err, errTooManyIdentifiers) {
		t.Fatalf("expected errTooManyIdentifiers, got %v", err)
	}

	normalized, err := normalizeResetPasskeysTarget(resetPasskeysTarget{Email: "  USER@Example.com  "})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if normalized.Email != "user@example.com" {
		t.Fatalf("expected normalized email, got %q", normalized.Email)
	}
}

func TestResetUserPasskeysByEmail(t *testing.T) {
	setupCommandTestDB(t)
	user := createLocalTestUser(t, "user@example.com")

	updatedUser, enrollmentToken, err := resetUserPasskeys(t.Context(), resetPasskeysTarget{Email: "USER@EXAMPLE.COM"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if enrollmentToken == "" {
		t.Fatal("expected generated password")
	}

	if updatedUser.Id != user.Id {
		t.Fatalf("expected user id %q, got %q", user.Id, updatedUser.Id)
	}

	storedUser, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to load updated user: %v", err)
	}

	enrollment, err := gorm.G[models.PasskeyEnrollment](db.DB).Where("user_id = ?", user.Id).First(t.Context())
	if err != nil || enrollment.TokenHash != passkeys.TokenHash(enrollmentToken) {
		t.Fatal("expected enrollment code digest")
	}

	if storedUser.TokenVersion != user.TokenVersion+1 {
		t.Fatalf("expected token version %d after reset, got %d", user.TokenVersion+1, storedUser.TokenVersion)
	}
}

func TestResetUserPasskeysRejectsManagedUser(t *testing.T) {
	setupCommandTestDB(t)

	managedUser := models.User{
		Name:  "OIDC User",
		Email: "oidc@example.com",
	}

	if err := gorm.G[models.User](db.DB).Create(t.Context(), &managedUser); err != nil {
		t.Fatalf("failed to create managed user: %v", err)
	}

	_, _, err := resetUserPasskeys(t.Context(), resetPasskeysTarget{Email: managedUser.Email})
	if !errors.Is(err, errManagedUser) {
		t.Fatalf("expected errManagedUser, got %v", err)
	}
}

func TestRecoveryRevokesCredentialsSessionsAndPreviousCode(t *testing.T) {
	setupCommandTestDB(t)
	user := createLocalTestUser(t, "user@example.com")
	key := models.Passkey{UserId: user.Id, CredentialId: "credential", Name: "Laptop"}
	if err := gorm.G[models.Passkey](db.DB).Create(t.Context(), &key); err != nil {
		t.Fatal(err)
	}
	_, first, err := resetUserPasskeys(t.Context(), resetPasskeysTarget{ID: user.Id})
	if err != nil {
		t.Fatal(err)
	}
	updated, second, err := resetUserPasskeys(t.Context(), resetPasskeysTarget{ID: user.Id})
	if err != nil {
		t.Fatal(err)
	}
	if first == second || updated.TokenVersion != user.TokenVersion+2 {
		t.Fatal("recovery did not revoke previous sessions")
	}
	count, err := gorm.G[models.Passkey](db.DB).Where("user_id = ?", user.Id).Count(t.Context(), "*")
	if err != nil || count != 0 {
		t.Fatal("recovery did not revoke credentials")
	}
	if err := passkeys.ConsumeEnrollment(t.Context(), db.DB, &updated, passkeys.TokenHash(first)); err == nil {
		t.Fatal("old code remains valid")
	}
}
