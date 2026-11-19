package passkeys

import (
	"bytes"
	"encoding/base64"
	"errors"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestOriginAndVerificationPolicy(t *testing.T) {
	for _, origin := range []string{"https://orca.example.com:8443", "http://localhost:3000"} {
		wa, err := New(origin)
		if err != nil {
			t.Fatalf("%s: %v", origin, err)
		}
		options, _, err := wa.BeginRegistration(User{Id: "opaque-user-id", Email: "test@example.com", Name: "Test"})
		if err != nil {
			t.Fatal(err)
		}
		selection := options.Response.AuthenticatorSelection
		if selection.ResidentKey != protocol.ResidentKeyRequirementRequired || selection.UserVerification != protocol.VerificationRequired {
			t.Fatalf("unsafe authenticator selection: %+v", selection)
		}
		login, session, err := wa.BeginDiscoverableLogin()
		if err != nil {
			t.Fatal(err)
		}
		if len(login.Response.AllowedCredentials) != 0 || session.UserVerification != protocol.VerificationRequired {
			t.Fatal("expected discoverable login with verification required")
		}
	}
	for _, origin := range []string{"", "http://127.0.0.1:8090", "http://orca.example.com", "ftp://localhost", "https://user:password@orca.example.com", "https://orca.example.com/path"} {
		if _, err := New(origin); err == nil {
			t.Fatalf("accepted unsafe origin %q", origin)
		}
	}
}

func TestSessionsSingleUseAndPurposeBinding(t *testing.T) {
	var sessions Sessions
	id, err := sessions.Put(Ceremony{Purpose: "login"})
	if err != nil {
		t.Fatal(err)
	}
	rawID, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(rawID) != 64 {
		t.Fatalf("ceremony ID must contain 64 random bytes: length=%d err=%v", len(rawID), err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, ok := sessions.Take(id, "login"); ok {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("expected one consumer, got %d", wins.Load())
	}
	id, _ = sessions.Put(Ceremony{Purpose: "setup"})
	if _, ok := sessions.Take(id, "login"); ok {
		t.Fatal("accepted a different purpose")
	}
	if _, ok := sessions.Take(id, "setup"); ok {
		t.Fatal("reused a failed ceremony")
	}
	id, _ = sessions.Put(Ceremony{Purpose: "login"})
	entry := sessions.entries[id]
	entry.expires = time.Now().Add(-time.Second)
	sessions.entries[id] = entry
	if _, ok := sessions.Take(id, "login"); ok {
		t.Fatal("accepted expired challenge")
	}
	for range maxSessions {
		if _, err := sessions.Put(Ceremony{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sessions.Put(Ceremony{}); err == nil {
		t.Fatal("session store is unbounded")
	}
}

func TestEnrollmentTokenHashUsesSHA512(t *testing.T) {
	const want = "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2" +
		"192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f"
	if got := TokenHash("abc"); got != want {
		t.Fatalf("unexpected SHA-512 digest: %s", got)
	}
}

func TestEnrollmentAndCredentialStorage(t *testing.T) {
	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatal(err)
	}
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.AutoMigrate(&models.User{}, &models.Passkey{}, &models.PasskeyEnrollment{}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	u := models.User{Name: "Test", Email: "test@example.com"}
	if err := gorm.G[models.User](database).Create(ctx, &u); err != nil {
		t.Fatal(err)
	}
	first, err := IssueEnrollment(ctx, database, &u)
	if err != nil {
		t.Fatal(err)
	}
	second, err := IssueEnrollment(ctx, database, &u)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("enrollment tokens must be random")
	}
	rawToken, err := base64.RawURLEncoding.DecodeString(second)
	if err != nil || len(rawToken) != 64 {
		t.Fatalf("enrollment token must contain 64 random bytes: length=%d err=%v", len(rawToken), err)
	}
	if err := ConsumeEnrollment(ctx, database, &u, TokenHash(first)); err == nil {
		t.Fatal("accepted replaced enrollment code")
	}
	u.TokenVersion++
	if err := ConsumeEnrollment(ctx, database, &u, TokenHash(second)); err == nil {
		t.Fatal("accepted code for old token version")
	}
	u.TokenVersion--
	if err := ConsumeEnrollment(ctx, database, &u, TokenHash(second)); err != nil {
		t.Fatal(err)
	}
	if err := ConsumeEnrollment(ctx, database, &u, TokenHash(second)); err == nil {
		t.Fatal("accepted code twice")
	}
	token, err := IssueEnrollment(ctx, database, &u)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&models.PasskeyEnrollment{}).Where("user_id = ?", u.Id).Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := ConsumeEnrollment(ctx, database, &u, TokenHash(token)); err == nil {
		t.Fatal("accepted expired code")
	}
	credential := &webauthn.Credential{ID: []byte("credential-id"), PublicKey: []byte("public-key")}
	if err := SaveCredential(ctx, database, u.Id, "Laptop", credential); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadUser(ctx, database, u.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Credentials) != 1 || string(loaded.Credentials[0].PublicKey) != "public-key" {
		t.Fatal("credential did not round trip")
	}
	var raw string
	if err := database.Raw("SELECT credential FROM passkeys").Scan(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if raw == "" || raw[0] == '{' {
		t.Fatal("credential must be encrypted at rest")
	}
	credential.Authenticator.SignCount = 42
	if err := UpdateCredential(ctx, database, u.Id, credential); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadUser(ctx, database, u.Id)
	if err != nil || loaded.Credentials[0].Authenticator.SignCount != 42 {
		t.Fatal("assertion state was not persisted")
	}
	if _, err := gorm.G[models.Passkey](database).Where("user_id = ?", u.Id).Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := UpdateCredential(ctx, database, u.Id, credential); err == nil {
		t.Fatal("recreated deleted credential")
	}
}

func credentialTestDB(t *testing.T) (*gorm.DB, models.User) {
	t.Helper()
	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatal(err)
	}
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "credentials.db")+"?_txlock=immediate&_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(10)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.AutoMigrate(&models.User{}, &models.Passkey{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Name: "Test", Email: "test@example.com"}
	if err := gorm.G[models.User](database).Create(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	return database, user
}

func TestSaveCredentialEnforcesPerUserLimit(t *testing.T) {
	database, user := credentialTestDB(t)
	for i := range MaxCredentials {
		if err := SaveCredential(t.Context(), database, user.Id, "Key", &webauthn.Credential{ID: []byte(strconv.Itoa(i))}); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveCredential(t.Context(), database, user.Id, "Extra", &webauthn.Credential{ID: []byte("extra")}); !errors.Is(err, ErrCredentialLimit) {
		t.Fatalf("expected credential limit, got %v", err)
	}
	count, err := gorm.G[models.Passkey](database).Where("user_id = ?", user.Id).Count(t.Context(), "*")
	if err != nil || count != MaxCredentials {
		t.Fatalf("unexpected credential count: %d, %v", count, err)
	}
	other := models.User{Name: "Other", Email: "other@example.com"}
	if err := gorm.G[models.User](database).Create(t.Context(), &other); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredential(t.Context(), database, other.Id, "Other key", &webauthn.Credential{ID: []byte("other")}); err != nil {
		t.Fatalf("one user's limit blocked another: %v", err)
	}
}

func TestConcurrentCredentialRegistrationsCannotExceedLimit(t *testing.T) {
	database, user := credentialTestDB(t)
	for i := range MaxCredentials - 1 {
		if err := SaveCredential(t.Context(), database, user.Id, "Key", &webauthn.Credential{ID: []byte(strconv.Itoa(i))}); err != nil {
			t.Fatal(err)
		}
	}
	const contenders = 8
	start := make(chan struct{})
	results := make(chan error, contenders)
	var workers sync.WaitGroup
	for i := range contenders {
		workers.Go(func() {
			<-start
			results <- SaveCredential(t.Context(), database, user.Id, "Concurrent", &webauthn.Credential{ID: []byte("concurrent-" + strconv.Itoa(i))})
		})
	}
	close(start)
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrCredentialLimit) {
			t.Fatalf("unexpected registration error: %v", err)
		}
	}
	count, err := gorm.G[models.Passkey](database).Where("user_id = ?", user.Id).Count(t.Context(), "*")
	if err != nil || successes != 1 || count != MaxCredentials {
		t.Fatalf("limit was not atomic: successes=%d count=%d err=%v", successes, count, err)
	}
}

func TestSaveCredentialPreservesCallerTransactionAndRejectsDuplicates(t *testing.T) {
	database, user := credentialTestDB(t)
	credential := &webauthn.Credential{ID: []byte("key")}
	rollback := errors.New("rollback account creation")
	err := database.Transaction(func(tx *gorm.DB) error {
		if err := SaveCredential(t.Context(), tx, user.Id, "Key", credential); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	count, err := gorm.G[models.Passkey](database).Count(t.Context(), "*")
	if err != nil || count != 0 {
		t.Fatal("credential escaped the caller's rollback")
	}
	if err := SaveCredential(t.Context(), database, user.Id, "Key", credential); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredential(t.Context(), database, user.Id, "Duplicate", credential); err == nil {
		t.Fatal("accepted a duplicate credential ID")
	}
}

func TestLoadUserSkipsCorruptCredentialsAndLogsTheirIDs(t *testing.T) {
	database, user := credentialTestDB(t)
	var logs bytes.Buffer
	database.Logger = gormlogger.New(log.New(&logs, "", 0), gormlogger.Config{LogLevel: gormlogger.Error})
	for _, id := range []string{"healthy", "backup", "invalid-ciphertext"} {
		if err := SaveCredential(t.Context(), database, user.Id, id, &webauthn.Credential{ID: []byte(id), PublicKey: []byte("public-key")}); err != nil {
			t.Fatal(err)
		}
	}
	malformed := models.Passkey{UserId: user.Id, Name: "Malformed", CredentialId: "malformed", Credential: crypto.EncryptedString("{not-json-sensitive}")}
	if err := gorm.G[models.Passkey](database).Create(t.Context(), &malformed); err != nil {
		t.Fatal(err)
	}
	// Bypass EncryptedString.Value to simulate damaged ciphertext on disk.
	if err := database.Exec("UPDATE passkeys SET credential = ? WHERE credential_id = ?", "damaged-ciphertext-sensitive", CredentialID([]byte("invalid-ciphertext"))).Error; err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadUser(t.Context(), database, user.Id)
	if err != nil || len(loaded.Credentials) != 2 {
		t.Fatalf("a damaged row hid healthy credentials: count=%d err=%v", len(loaded.Credentials), err)
	}
	for _, credential := range loaded.Credentials {
		if string(credential.ID) != "healthy" && string(credential.ID) != "backup" {
			t.Fatal("loaded a corrupt credential")
		}
	}
	if strings.Count(logs.String(), "skipping unreadable passkey") != 2 || !strings.Contains(logs.String(), malformed.Id) || !strings.Contains(logs.String(), user.Id) {
		t.Fatalf("missing corruption diagnostics: %s", logs.String())
	}
	for _, sensitive := range []string{"{not-json-sensitive}", "damaged-ciphertext-sensitive", "public-key"} {
		if strings.Contains(logs.String(), sensitive) {
			t.Fatal("credential contents appeared in diagnostics")
		}
	}
	if _, err := gorm.G[models.Passkey](database).Where("credential_id IN ?", []string{CredentialID([]byte("healthy")), CredentialID([]byte("backup"))}).Delete(t.Context()); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadUser(t.Context(), database, user.Id)
	if err != nil || len(loaded.Credentials) != 0 {
		t.Fatal("an account with only corrupt rows must have no usable credentials")
	}
}

func TestRecoveryRejectsDeletedAndManagedAccounts(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "recovery.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.AutoMigrate(&models.User{}, &models.Passkey{}, &models.PasskeyEnrollment{}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	managed := models.User{Name: "Managed", Email: "managed@example.com"}
	if err := gorm.G[models.User](database).Create(ctx, &managed); err != nil {
		t.Fatal(err)
	}
	if _, err := ResetLocal(ctx, database, &managed); !errors.Is(err, ErrManagedUser) {
		t.Fatalf("expected ErrManagedUser for an SSO-only account, got %v", err)
	}
	local := models.User{Name: "Local", Email: "local@example.com", LocalAuthEnabled: true}
	if err := gorm.G[models.User](database).Create(ctx, &local); err != nil {
		t.Fatal(err)
	}
	if _, err := gorm.G[models.User](database).Where("id = ?", local.Id).Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ResetLocal(ctx, database, &local); err == nil {
		t.Fatal("recovered a deleted account")
	}
	count, err := gorm.G[models.PasskeyEnrollment](database).Count(ctx, "*")
	if err != nil || count != 0 {
		t.Fatal("failed recovery issued an enrollment code")
	}
	local.Id = ""
	if err := gorm.G[models.User](database).Create(ctx, &local); err != nil {
		t.Fatal(err)
	}
	stale := local
	if _, err := gorm.G[models.User](database).Where("id = ?", local.Id).Update(ctx, "token_version", 7); err != nil {
		t.Fatal(err)
	}
	var code string
	if err := database.Transaction(func(tx *gorm.DB) error {
		var err error
		code, err = ResetLocal(ctx, tx, &stale)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if stale.TokenVersion != 8 {
		t.Fatal("recovery used the caller's stale session version")
	}
	if err := ConsumeEnrollment(ctx, database, &stale, TokenHash(code)); err != nil {
		t.Fatal(err)
	}
}
