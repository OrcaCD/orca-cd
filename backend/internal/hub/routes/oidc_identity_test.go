package routes

import (
	"fmt"
	"slices"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/oidc"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"gorm.io/gorm"
)

func TestResolveOIDCUser_CreatesNewUserAndIdentity(t *testing.T) {
	setupTestDB(t)

	provider := models.OIDCProvider{
		Id:         "provider-1",
		AutoSignup: true,
	}
	claims := &oidc.OIDCUser{
		Issuer:  "https://issuer-1.example.com",
		Subject: "subject-1",
		Email:   "alice@example.com",
		Name:    "Alice",
	}

	user, _, redirectError := resolveOIDCUser(t.Context(), &provider, claims)
	if redirectError != "" {
		t.Fatalf("expected no redirect error, got %q", redirectError)
	}
	if user.Email != claims.Email {
		t.Fatalf("expected email %q, got %q", claims.Email, user.Email)
	}

	identity, err := gorm.G[models.UserOIDCIdentity](db.DB).Where("provider_id = ? AND subject = ?", provider.Id, claims.Subject).First(t.Context())
	if err != nil {
		t.Fatalf("expected linked identity row, got error: %v", err)
	}
	if identity.UserId != user.Id {
		t.Fatalf("expected identity to point at user %q, got %q", user.Id, identity.UserId)
	}
	if identity.ProviderId != provider.Id {
		t.Fatalf("expected provider id %q, got %v", provider.Id, identity.ProviderId)
	}
}

func TestResolveOIDCUser_LinksMultipleProvidersToSameUser(t *testing.T) {
	setupTestDB(t)

	provider1 := models.OIDCProvider{
		Id:         "provider-1",
		AutoSignup: true,
	}
	provider2 := models.OIDCProvider{
		Id:         "provider-2",
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

	firstUser, _, redirectError := resolveOIDCUser(t.Context(), &provider1, firstClaims)
	if redirectError != "" {
		t.Fatalf("expected no redirect error on first login, got %q", redirectError)
	}

	secondUser, _, redirectError := resolveOIDCUser(t.Context(), &provider2, secondClaims)
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
		providerIds[identity.ProviderId] = true
	}
	if !providerIds[provider1.Id] || !providerIds[provider2.Id] {
		t.Fatalf("expected identities for providers %q and %q", provider1.Id, provider2.Id)
	}
}

func TestResolveOIDCUser_RejectsProviderEmailConflict(t *testing.T) {
	setupTestDB(t)

	provider := models.OIDCProvider{
		Id:         "provider-1",
		AutoSignup: true,
	}

	userA := models.User{Email: "alice@example.com", Name: "Alice"}
	if err := gorm.G[models.User](db.DB).Create(t.Context(), &userA); err != nil {
		t.Fatalf("failed to create user A: %v", err)
	}
	userB := models.User{Email: "bob@example.com", Name: "Bob"}
	if err := gorm.G[models.User](db.DB).Create(t.Context(), &userB); err != nil {
		t.Fatalf("failed to create user B: %v", err)
	}

	identityA := models.UserOIDCIdentity{
		UserId:     userA.Id,
		ProviderId: provider.Id,
		Subject:    "subject-1",
	}
	if err := gorm.G[models.UserOIDCIdentity](db.DB).Create(t.Context(), &identityA); err != nil {
		t.Fatalf("failed to create identity A: %v", err)
	}

	identityB := models.UserOIDCIdentity{
		UserId:     userB.Id,
		ProviderId: provider.Id,
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

	_, _, redirectError := resolveOIDCUser(t.Context(), &provider, claims)
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
		Id:         "provider-1",
		AutoSignup: false,
	}
	claims := &oidc.OIDCUser{
		Issuer:  "https://issuer-1.example.com",
		Subject: "subject-1",
		Email:   "alice@example.com",
		Name:    "Alice",
	}

	_, _, redirectError := resolveOIDCUser(t.Context(), &provider, claims)
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

func TestResolveOIDCUser_LinksAdminCreatedUser(t *testing.T) {
	setupTestDB(t)

	hash := "hash"
	existing := models.User{Id: "user-1", Email: "alice@example.com", Name: "Alice", PasswordHash: &hash}
	if err := db.DB.Create(&existing).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	provider := models.OIDCProvider{Id: "provider-1"}
	claims := &oidc.OIDCUser{Subject: "subject-1", Email: "alice@example.com", Name: "Alice SSO"}

	user, _, redirectError := resolveOIDCUser(t.Context(), &provider, claims)
	if redirectError != "" {
		t.Fatalf("expected no redirect error, got %q", redirectError)
	}
	if user.Id != existing.Id {
		t.Fatalf("expected login to be linked to user %q, got %q", existing.Id, user.Id)
	}

	reloaded, err := gorm.G[models.User](db.DB).Where("id = ?", existing.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if reloaded.Name != claims.Name {
		t.Fatalf("expected name to be synced to %q, got %q", claims.Name, reloaded.Name)
	}
}

func TestResolveOIDCUser_RefusesToLinkSelfChangedEmail(t *testing.T) {
	setupTestDB(t)

	hash := "hash"
	existing := models.User{Id: "user-1", Email: "alice@example.com", Name: "Mallory", PasswordHash: &hash, EmailUnconfirmed: true}
	if err := db.DB.Create(&existing).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	provider := models.OIDCProvider{Id: "provider-1", AutoSignup: true}
	claims := &oidc.OIDCUser{Subject: "subject-1", Email: "alice@example.com", Name: "Alice"}

	_, _, redirectError := resolveOIDCUser(t.Context(), &provider, claims)
	if redirectError != "account_linking_failed" {
		t.Fatalf("expected account_linking_failed, got %q", redirectError)
	}
	count, err := gorm.G[models.UserOIDCIdentity](db.DB).Where("user_id = ?", existing.Id).Count(t.Context(), "*")
	if err != nil {
		t.Fatalf("failed to count identities: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no identity to be linked, got %d", count)
	}
}

func TestResolveOIDCUser_LinkedLoginConfirmsSyncedEmail(t *testing.T) {
	setupTestDB(t)

	existing := models.User{Id: "user-1", Email: "old@example.com", Name: "Alice", EmailUnconfirmed: true}
	if err := db.DB.Create(&existing).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	identity := models.UserOIDCIdentity{UserId: existing.Id, ProviderId: "provider-1", Subject: "subject-1"}
	if err := db.DB.Create(&identity).Error; err != nil {
		t.Fatalf("failed to create identity: %v", err)
	}

	provider := models.OIDCProvider{Id: "provider-1"}
	claims := &oidc.OIDCUser{Subject: "subject-1", Email: "alice@example.com", Name: "Alice"}
	if _, _, redirectError := resolveOIDCUser(t.Context(), &provider, claims); redirectError != "" {
		t.Fatalf("expected no redirect error, got %q", redirectError)
	}

	reloaded, err := gorm.G[models.User](db.DB).Where("id = ?", existing.Id).First(t.Context())
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if reloaded.Email != claims.Email || reloaded.EmailUnconfirmed {
		t.Fatalf("expected the provider email to be synced and confirmed, got %q (unconfirmed=%v)", reloaded.Email, reloaded.EmailUnconfirmed)
	}
}

func TestResolveOIDCUser_DefaultRoleUnlessGroupsMapToARole(t *testing.T) {
	setupRolesTestDB(t)

	provider := createTestProvider(t, "Corp", true)
	if _, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ?", provider.Id).Update(t.Context(), "groups_claim", "groups"); err != nil {
		t.Fatalf("failed to enable groups: %v", err)
	}
	devs := models.Role{Name: "Devs"}
	if err := gorm.G[models.Role](db.DB).Create(t.Context(), &devs); err != nil {
		t.Fatalf("failed to create role: %v", err)
	}
	if err := gorm.G[models.RoleOIDCGroup](db.DB).Create(t.Context(), &models.RoleOIDCGroup{RoleId: devs.Id, ProviderId: provider.Id, Group: "devs"}); err != nil {
		t.Fatalf("failed to map group: %v", err)
	}

	tests := []struct {
		name        string
		groupsClaim string
		groups      []string
		wantDefault bool
		wantDevs    bool
	}{
		{"no mapped group", "groups", []string{"other"}, true, false},
		{"mapped group", "groups", []string{"devs"}, false, true},
		{"sync disabled", "", []string{"devs"}, true, false},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider.GroupsClaim = tt.groupsClaim
			if _, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ?", provider.Id).Update(t.Context(), "groups_claim", tt.groupsClaim); err != nil {
				t.Fatalf("failed to set groups claim: %v", err)
			}
			email := fmt.Sprintf("user-%d@example.com", i)
			user, rolesChanged, redirectError := resolveOIDCUser(t.Context(), &provider, &oidc.OIDCUser{
				Subject: "subject-" + tt.name,
				Email:   email,
				Name:    "User",
				Groups:  tt.groups,
			})
			if redirectError != "" {
				t.Fatalf("expected no redirect error, got %q", redirectError)
			}
			if rolesChanged != tt.wantDevs {
				t.Fatalf("expected rolesChanged=%v, got %v", tt.wantDevs, rolesChanged)
			}

			roles, err := gorm.G[models.UserRole](db.DB).Where("user_id = ?", user.Id).Find(t.Context())
			if err != nil {
				t.Fatalf("failed to load roles: %v", err)
			}
			hasDefault := slices.ContainsFunc(roles, func(r models.UserRole) bool { return r.RoleId == rbac.UserRoleID })
			hasDevs := slices.ContainsFunc(roles, func(r models.UserRole) bool {
				return r.RoleId == devs.Id && r.Source == models.UserRoleSourceOIDC
			})
			if hasDefault != tt.wantDefault || hasDevs != tt.wantDevs {
				t.Fatalf("expected default=%v devs=%v, got %+v", tt.wantDefault, tt.wantDevs, roles)
			}
		})
	}

	// Leaving the group removes the role on the next login
	provider.GroupsClaim = "groups"
	if _, err := gorm.G[models.OIDCProvider](db.DB).Where("id = ?", provider.Id).Update(t.Context(), "groups_claim", "groups"); err != nil {
		t.Fatalf("failed to set groups claim: %v", err)
	}
	user, rolesChanged, redirectError := resolveOIDCUser(t.Context(), &provider, &oidc.OIDCUser{Subject: "subject-mapped group", Email: "user-1@example.com", Name: "User", Groups: []string{}})
	if redirectError != "" || !rolesChanged {
		t.Fatalf("expected the roles to change, got changed=%v error=%q", rolesChanged, redirectError)
	}
	identity, err := gorm.G[models.UserOIDCIdentity](db.DB).Where("user_id = ?", user.Id).First(t.Context())
	if err != nil || len(identity.Groups) != 0 {
		t.Fatalf("expected the stored groups to be cleared, got %+v (err=%v)", identity, err)
	}
}
