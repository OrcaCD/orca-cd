package routes

import (
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/oidc"
	"gorm.io/gorm"
)

func TestResolveOIDCUser_CreatesNewUserAndIdentity(t *testing.T) {
	setupTestDB(t)

	provider := models.OIDCProvider{
		Base:       models.Base{Id: "provider-1"},
		AutoSignup: true,
	}
	claims := &oidc.OIDCUser{
		Issuer:  "https://issuer-1.example.com",
		Subject: "subject-1",
		Email:   "alice@example.com",
		Name:    "Alice",
	}

	user, redirectError := resolveOIDCUser(t.Context(), &provider, claims)
	if redirectError != "" {
		t.Fatalf("expected no redirect error, got %q", redirectError)
	}
	if user.Email != claims.Email {
		t.Fatalf("expected email %q, got %q", claims.Email, user.Email)
	}

	identity, err := gorm.G[models.UserOIDCIdentity](db.DB).Where("issuer = ? AND subject = ?", claims.Issuer, claims.Subject).First(t.Context())
	if err != nil {
		t.Fatalf("expected linked identity row, got error: %v", err)
	}
	if identity.UserId != user.Id {
		t.Fatalf("expected identity to point at user %q, got %q", user.Id, identity.UserId)
	}
	if identity.ProviderId == nil || *identity.ProviderId != provider.Id {
		t.Fatalf("expected provider id %q, got %v", provider.Id, identity.ProviderId)
	}
}

func TestResolveOIDCUser_LinksMultipleProvidersToSameUser(t *testing.T) {
	setupTestDB(t)

	provider1 := models.OIDCProvider{
		Base:       models.Base{Id: "provider-1"},
		AutoSignup: true,
	}
	provider2 := models.OIDCProvider{
		Base:       models.Base{Id: "provider-2"},
		AutoSignup: true,
	}

	firstClaims := &oidc.OIDCUser{
		Issuer:  "https://issuer-1.example.com",
		Subject: "subject-1",
		Email:   "alice@example.com",
		Name:    "Alice",
	}
	secondClaims := &oidc.OIDCUser{
		Issuer:  "https://issuer-2.example.com",
		Subject: "subject-2",
		Email:   "alice@example.com",
		Name:    "Alice A.",
	}

	firstUser, redirectError := resolveOIDCUser(t.Context(), &provider1, firstClaims)
	if redirectError != "" {
		t.Fatalf("expected no redirect error on first login, got %q", redirectError)
	}

	secondUser, redirectError := resolveOIDCUser(t.Context(), &provider2, secondClaims)
	if redirectError != "" {
		t.Fatalf("expected no redirect error on second login, got %q", redirectError)
	}

	if firstUser.Id != secondUser.Id {
		t.Fatalf("expected both providers to link to the same user, got %q and %q", firstUser.Id, secondUser.Id)
	}

	identities, err := gorm.G[models.UserOIDCIdentity](db.DB).Where("user_id = ?", firstUser.Id).Find(t.Context())
	if err != nil {
		t.Fatalf("failed to load linked identities: %v", err)
	}
	if len(identities) != 2 {
		t.Fatalf("expected 2 linked identities, got %d", len(identities))
	}

	providerIds := map[string]bool{}
	for _, identity := range identities {
		if identity.ProviderId != nil {
			providerIds[*identity.ProviderId] = true
		}
	}
	if !providerIds[provider1.Id] || !providerIds[provider2.Id] {
		t.Fatalf("expected identities for providers %q and %q", provider1.Id, provider2.Id)
	}
}

func TestResolveOIDCUser_RejectsProviderEmailConflict(t *testing.T) {
	setupTestDB(t)

	provider := models.OIDCProvider{
		Base:       models.Base{Id: "provider-1"},
		AutoSignup: true,
	}

	userA := models.User{Email: "alice@example.com", Name: "Alice", Role: models.UserRoleUser}
	if err := gorm.G[models.User](db.DB).Create(t.Context(), &userA); err != nil {
		t.Fatalf("failed to create user A: %v", err)
	}
	userB := models.User{Email: "bob@example.com", Name: "Bob", Role: models.UserRoleUser}
	if err := gorm.G[models.User](db.DB).Create(t.Context(), &userB); err != nil {
		t.Fatalf("failed to create user B: %v", err)
	}

	providerId := provider.Id
	identityA := models.UserOIDCIdentity{
		UserId:     userA.Id,
		ProviderId: &providerId,
		Issuer:     "https://issuer-1.example.com",
		Subject:    "subject-1",
	}
	if err := gorm.G[models.UserOIDCIdentity](db.DB).Create(t.Context(), &identityA); err != nil {
		t.Fatalf("failed to create identity A: %v", err)
	}

	identityB := models.UserOIDCIdentity{
		UserId:     userB.Id,
		ProviderId: &providerId,
		Issuer:     "https://issuer-1.example.com",
		Subject:    "subject-2",
	}
	if err := gorm.G[models.UserOIDCIdentity](db.DB).Create(t.Context(), &identityB); err != nil {
		t.Fatalf("failed to create identity B: %v", err)
	}

	claims := &oidc.OIDCUser{
		Issuer:  "https://issuer-1.example.com",
		Subject: "subject-2",
		Email:   "alice@example.com",
		Name:    "Bob Updated",
	}

	_, redirectError := resolveOIDCUser(t.Context(), &provider, claims)
	if redirectError != "provider_email_conflict" {
		t.Fatalf("expected provider_email_conflict, got %q", redirectError)
	}

	loadedUserB, err := gorm.G[models.User](db.DB).Where("id = ?", userB.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to reload user B: %v", err)
	}
	if loadedUserB.Email != "bob@example.com" {
		t.Fatalf("expected user B email to remain unchanged, got %q", loadedUserB.Email)
	}
	if loadedUserB.Name != "Bob" {
		t.Fatalf("expected user B name to remain unchanged, got %q", loadedUserB.Name)
	}
}

func TestResolveOIDCUser_RejectsNewUserWhenSignupDisabled(t *testing.T) {
	setupTestDB(t)

	provider := models.OIDCProvider{
		Base:       models.Base{Id: "provider-1"},
		AutoSignup: false,
	}
	claims := &oidc.OIDCUser{
		Issuer:  "https://issuer-1.example.com",
		Subject: "subject-1",
		Email:   "alice@example.com",
		Name:    "Alice",
	}

	_, redirectError := resolveOIDCUser(t.Context(), &provider, claims)
	if redirectError != "signup_disabled" {
		t.Fatalf("expected signup_disabled, got %q", redirectError)
	}

	userCount, err := gorm.G[models.User](db.DB).Count(t.Context(), "*")
	if err != nil {
		t.Fatalf("failed to count users: %v", err)
	}
	if userCount != 0 {
		t.Fatalf("expected no users to be created, got %d", userCount)
	}
}
