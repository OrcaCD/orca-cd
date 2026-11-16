package passkeys

import (
	"path/filepath"
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
	if first == second || len(second) < 40 {
		t.Fatal("enrollment tokens must be random")
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
