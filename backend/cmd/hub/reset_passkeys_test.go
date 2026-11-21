package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/passkeys"
	"github.com/rs/zerolog"
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
		t.Fatal("expected enrollment code")
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

func TestResetPasskeysCommandIssuesUsableCode(t *testing.T) {
	setupKeyRotateCommandEnv(t)
	t.Setenv("DEMO", "false")
	logger := zerolog.New(io.Discard)
	if err := db.Connect(logger, zerolog.Disabled, false); err != nil {
		t.Fatal(err)
	}
	user := createLocalTestUser(t, "recovery@example.com")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := newResetPasskeysCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--email", " RECOVERY@EXAMPLE.COM "})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Passkey Recovery Ready") || !strings.Contains(out.String(), user.Email) || !strings.Contains(out.String(), "24 hours") {
		t.Fatalf("missing recovery instructions: %q", out.String())
	}
	if db.DB != nil {
		t.Fatal("command did not close its database")
	}
	if err := db.Connect(logger, zerolog.Disabled, false); err != nil {
		t.Fatal(err)
	}
	enrollment, err := gorm.G[models.PasskeyEnrollment](db.DB).Where("user_id = ?", user.Id).First(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var token string
	for word := range strings.FieldsSeq(out.String()) {
		if passkeys.TokenHash(word) == enrollment.TokenHash {
			token = word
			break
		}
	}
	if token == "" {
		t.Fatal("printed code does not match stored enrollment")
	}
	updated, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).First(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := passkeys.ConsumeEnrollment(t.Context(), db.DB, &updated, passkeys.TokenHash(token)); err != nil {
		t.Fatal(err)
	}
}

func TestResetPasskeysCommandInitializesCipherBeforeConnecting(t *testing.T) {
	setupKeyRotateCommandEnv(t)
	t.Setenv("DEMO", "false")
	t.Setenv("APP_SECRET", testOldAppSecret)
	seedKeyRotateCommandDatabase(t)
	if err := db.Connect(zerolog.New(io.Discard), zerolog.Disabled, false); err != nil {
		t.Fatal(err)
	}
	user := createLocalTestUser(t, "recovery@example.com")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	clearDefaultCipher(t)

	if err := runResetPasskeysCommand(t.Context(), io.Discard, resetPasskeysTarget{Email: user.Email}); err != nil {
		t.Fatalf("runResetPasskeysCommand() unexpected error: %v", err)
	}
}

func TestResetPasskeysCommandRejectsInvalidInputsAndEnvironment(t *testing.T) {
	for _, scenario := range []string{"missing identifier", "invalid configuration", "demo", "missing database", "unknown user"} {
		t.Run(scenario, func(t *testing.T) {
			dir := setupKeyRotateCommandEnv(t)
			t.Setenv("DEMO", "false")
			target := resetPasskeysTarget{ID: "missing"}
			want := "reset passkeys"
			switch scenario {
			case "missing identifier":
				target = resetPasskeysTarget{}
				want = "must be provided"
			case "invalid configuration":
				t.Setenv("APP_SECRET", "")
				want = "configuration"
			case "demo":
				t.Setenv("DEMO", "true")
				want = "demo mode"
			case "missing database":
				if err := os.RemoveAll(filepath.Join(dir, "data")); err != nil {
					t.Fatal(err)
				}
				want = "connect to database"
			case "unknown user":
				want = "user not found with id \"missing\""
			}
			err := runResetPasskeysCommand(t.Context(), io.Discard, target)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected %q, got %v", want, err)
			}
		})
	}
}

func TestResetUserPasskeysRejectsUnknownUser(t *testing.T) {
	for _, test := range []struct {
		target resetPasskeysTarget
		want   string
	}{
		{resetPasskeysTarget{ID: " missing-id "}, `user not found with id "missing-id"`},
		{resetPasskeysTarget{Email: " MISSING@EXAMPLE.COM "}, `user not found with email "missing@example.com"`},
	} {
		t.Run(test.want, func(t *testing.T) {
			setupCommandTestDB(t)
			user, code, err := resetUserPasskeys(t.Context(), test.target)
			if err == nil || err.Error() != test.want || user.Id != "" || code != "" {
				t.Fatalf("expected %q without recovery data, got user=%q code=%q err=%v", test.want, user.Id, code, err)
			}
		})
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
	if !errors.Is(err, passkeys.ErrManagedUser) {
		t.Fatalf("expected ErrManagedUser, got %v", err)
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
