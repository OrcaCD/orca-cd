package rbac

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

//go:fix inline

func grant(resource models.PermissionResource, action models.PermissionAction, resourceID *string) models.RolePermission {
	return models.RolePermission{Resource: resource, Action: action, ResourceId: resourceID}
}

func TestPermissions_Admin(t *testing.T) {
	p := Admin()
	if !p.IsAdmin() {
		t.Fatal("expected admin")
	}
	if !p.Can(models.ResourceApplication, models.ActionDeploy, "any") || !p.CanAll(models.ResourceAgent, models.ActionDelete) {
		t.Fatal("expected admin to be allowed everything")
	}
	if ids, all := p.IDs(models.ResourceRepository, models.ActionRead); !all || ids != nil {
		t.Fatalf("expected all repositories, got ids=%v all=%v", ids, all)
	}
}

func TestPermissions_NilAndEmptyDenyEverything(t *testing.T) {
	for name, p := range map[string]*Permissions{"nil": nil, "empty": New(nil)} {
		t.Run(name, func(t *testing.T) {
			if p.IsAdmin() || p.Can(models.ResourceApplication, models.ActionRead, "app") || p.CanAny(models.ResourceApplication, models.ActionRead) {
				t.Fatal("expected no permissions")
			}
			if ids, all := p.IDs(models.ResourceApplication, models.ActionRead); all || len(ids) != 0 {
				t.Fatalf("expected no visible applications, got ids=%v all=%v", ids, all)
			}
		})
	}
}

func TestPermissions_GlobalGrant(t *testing.T) {
	p := New([]models.RolePermission{grant(models.ResourceApplication, models.ActionDeploy, nil)})

	if !p.Can(models.ResourceApplication, models.ActionDeploy, "app-1") || !p.CanAll(models.ResourceApplication, models.ActionDeploy) {
		t.Fatal("expected deploy on all applications")
	}
	if !p.CanAll(models.ResourceApplication, models.ActionRead) {
		t.Fatal("expected deploy to imply read")
	}
	if p.Can(models.ResourceApplication, models.ActionDelete, "app-1") {
		t.Fatal("expected delete to be denied")
	}
	if p.Can(models.ResourceRepository, models.ActionRead, "repo-1") {
		t.Fatal("expected other resource types to be denied")
	}
}

func TestPermissions_ScopedGrant(t *testing.T) {
	p := New([]models.RolePermission{
		grant(models.ResourceApplication, models.ActionDeploy, new("app-2")),
		grant(models.ResourceApplication, models.ActionRead, new("app-1")),
	})

	if !p.Can(models.ResourceApplication, models.ActionDeploy, "app-2") {
		t.Fatal("expected deploy on app-2")
	}
	if p.Can(models.ResourceApplication, models.ActionDeploy, "app-1") {
		t.Fatal("expected deploy on app-1 to be denied")
	}
	if p.Can(models.ResourceApplication, models.ActionDeploy, "") || p.CanAll(models.ResourceApplication, models.ActionRead) {
		t.Fatal("expected scoped grants not to match all applications")
	}
	if !p.CanAny(models.ResourceApplication, models.ActionDeploy) || p.CanAny(models.ResourceApplication, models.ActionDelete) {
		t.Fatal("unexpected CanAny result")
	}

	ids, all := p.IDs(models.ResourceApplication, models.ActionRead)
	if all || !slices.Equal(ids, []string{"app-1", "app-2"}) {
		t.Fatalf("expected deploy to imply read on app-2, got ids=%v all=%v", ids, all)
	}
}

func TestPermissions_IgnoresInvalidGrants(t *testing.T) {
	p := New([]models.RolePermission{
		grant(models.ResourceAgent, models.ActionDeploy, nil),
		grant(models.ResourceApplication, models.ActionCreate, new("app-1")),
	})
	if p.CanAny(models.ResourceAgent, models.ActionRead) || p.CanAny(models.ResourceApplication, models.ActionRead) {
		t.Fatal("expected invalid grants to be ignored")
	}
}

func TestPermissions_Grants(t *testing.T) {
	p := New([]models.RolePermission{
		grant(models.ResourceApplication, models.ActionRead, nil),
		grant(models.ResourceApplication, models.ActionRead, new("app-1")),
		grant(models.ResourceApplication, models.ActionDeploy, new("app-1")),
	})

	grants := p.Grants()
	app := grants[models.ResourceApplication]
	if !slices.Equal(app.All, []models.PermissionAction{models.ActionRead}) {
		t.Fatalf("expected read on all applications, got %v", app.All)
	}
	// Read is already granted on all applications and left out of the single resource.
	if !slices.Equal(app.Resources["app-1"], []models.PermissionAction{models.ActionDeploy}) {
		t.Fatalf("expected deploy on app-1, got %v", app.Resources["app-1"])
	}
	if len(grants) != len(models.PermissionResources) {
		t.Fatalf("expected a grant for every resource type, got %d", len(grants))
	}

	admin := Admin().Grants()[models.ResourceRepository]
	if !slices.Equal(admin.All, models.PermissionActions[models.ResourceRepository]) {
		t.Fatalf("expected admin to have every repository action, got %v", admin.All)
	}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	tx, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	if err := tx.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	if err := MigrateForTest(tx); err != nil {
		t.Fatalf("failed to migrate roles: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := tx.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return tx
}

func createUser(t *testing.T, tx *gorm.DB, id string, roleIDs ...string) {
	t.Helper()
	if err := gorm.G[models.User](tx).Create(t.Context(), &models.User{Id: id, Email: id + "@example.com", Name: id}); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if _, err := SetUserRoles(t.Context(), tx, id, models.UserRoleSourceManual, roleIDs); err != nil {
		t.Fatalf("failed to assign roles: %v", err)
	}
}

func TestLoad_UnionOfRoles(t *testing.T) {
	tx := openTestDB(t)

	roles := []models.Role{{Name: "Deployers"}, {Name: "Readers"}}
	for i := range roles {
		if err := gorm.G[models.Role](tx).Create(t.Context(), &roles[i]); err != nil {
			t.Fatalf("failed to create role: %v", err)
		}
	}
	permissions := []models.RolePermission{
		{RoleId: roles[0].Id, Resource: models.ResourceApplication, Action: models.ActionDeploy, ResourceId: new("app-1")},
		{RoleId: roles[1].Id, Resource: models.ResourceRepository, Action: models.ActionRead},
	}
	if err := gorm.G[models.RolePermission](tx).CreateInBatches(t.Context(), &permissions, 10); err != nil {
		t.Fatalf("failed to create permissions: %v", err)
	}
	createUser(t, tx, "alice", roles[0].Id, roles[1].Id)

	p, err := Load(t.Context(), tx, "alice")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if p.IsAdmin() {
		t.Fatal("expected alice not to be an admin")
	}
	if !p.Can(models.ResourceApplication, models.ActionDeploy, "app-1") || !p.CanAll(models.ResourceRepository, models.ActionRead) {
		t.Fatal("expected the permissions of both roles")
	}
	if p.CanAny(models.ResourceAgent, models.ActionRead) {
		t.Fatal("expected no access to agents")
	}
}

func TestLoad_AdminAndDefaultRoles(t *testing.T) {
	tx := openTestDB(t)
	createUser(t, tx, "admin", AdminRoleID)
	createUser(t, tx, "user", UserRoleID)
	createUser(t, tx, "nobody")

	admin, err := Load(t.Context(), tx, "admin")
	if err != nil || !admin.IsAdmin() {
		t.Fatalf("expected admin, err=%v", err)
	}

	user, err := Load(t.Context(), tx, "user")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	for _, resource := range models.PermissionResources {
		for _, action := range models.PermissionActions[resource] {
			if !user.CanAll(resource, action) {
				t.Errorf("expected the user role to allow %s on all %s", action, resource)
			}
		}
	}
	if user.IsAdmin() {
		t.Error("expected the user role not to be admin")
	}

	nobody, err := Load(t.Context(), tx, "nobody")
	if err != nil || nobody.CanAny(models.ResourceApplication, models.ActionRead) {
		t.Fatalf("expected a user without roles to have no access, err=%v", err)
	}
}

func TestCountAdmins(t *testing.T) {
	tx := openTestDB(t)
	createUser(t, tx, "admin-1", AdminRoleID)
	createUser(t, tx, "admin-2", AdminRoleID, UserRoleID)
	createUser(t, tx, "user", UserRoleID)
	// The same role from two sources still counts once.
	if _, err := SetUserRoles(t.Context(), tx, "admin-1", models.UserRoleSourceOIDC, []string{AdminRoleID}); err != nil {
		t.Fatalf("failed to assign oidc role: %v", err)
	}

	count, err := CountAdmins(t.Context(), tx)
	if err != nil || count != 2 {
		t.Fatalf("expected 2 admins, got %d (err=%v)", count, err)
	}
}

func TestSetUserRoles_KeepsOtherSources(t *testing.T) {
	tx := openTestDB(t)
	createUser(t, tx, "alice", UserRoleID)
	if _, err := SetUserRoles(t.Context(), tx, "alice", models.UserRoleSourceOIDC, []string{AdminRoleID}); err != nil {
		t.Fatalf("SetUserRoles() error: %v", err)
	}

	changed, err := SetUserRoles(t.Context(), tx, "alice", models.UserRoleSourceManual, nil)
	if err != nil || !changed {
		t.Fatalf("expected the manual role to be removed, changed=%v err=%v", changed, err)
	}
	changed, err = SetUserRoles(t.Context(), tx, "alice", models.UserRoleSourceManual, nil)
	if err != nil || changed {
		t.Fatalf("expected no change, changed=%v err=%v", changed, err)
	}

	assignments, err := gorm.G[models.UserRole](tx).Where("user_id = ?", "alice").Find(t.Context())
	if err != nil {
		t.Fatalf("failed to load roles: %v", err)
	}
	if len(assignments) != 1 || assignments[0].RoleId != AdminRoleID || assignments[0].Source != models.UserRoleSourceOIDC {
		t.Fatalf("expected only the oidc role to remain, got %+v", assignments)
	}
}

func TestLoad_ReturnsRolesWithSources(t *testing.T) {
	tx := openTestDB(t)
	createUser(t, tx, "alice", UserRoleID)
	if _, err := SetUserRoles(t.Context(), tx, "alice", models.UserRoleSourceOIDC, []string{UserRoleID, AdminRoleID}); err != nil {
		t.Fatalf("SetUserRoles() error: %v", err)
	}

	p, err := Load(t.Context(), tx, "alice")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !p.IsAdmin() {
		t.Fatal("expected the admin role from OIDC to make alice an admin")
	}
	roles := p.Roles()
	slices.SortFunc(roles, func(a, b AssignedRole) int { return strings.Compare(a.Id, b.Id) })
	want := []AssignedRole{
		{Id: AdminRoleID, Name: "Admin", BuiltIn: new(models.RoleBuiltInAdmin), Sources: []models.UserRoleSource{models.UserRoleSourceOIDC}},
		{Id: UserRoleID, Name: "User", BuiltIn: new(models.RoleBuiltInUser), Sources: []models.UserRoleSource{models.UserRoleSourceManual, models.UserRoleSourceOIDC}},
	}
	if len(roles) != len(want) {
		t.Fatalf("expected %d roles, got %+v", len(want), roles)
	}
	for i := range want {
		if roles[i].Id != want[i].Id || roles[i].Name != want[i].Name || *roles[i].BuiltIn != *want[i].BuiltIn || !slices.Equal(roles[i].Sources, want[i].Sources) {
			t.Errorf("role %d: expected %+v, got %+v", i, want[i], roles[i])
		}
	}

	createUser(t, tx, "nobody")
	nobody, err := Load(t.Context(), tx, "nobody")
	if err != nil || len(nobody.Roles()) != 0 {
		t.Fatalf("expected no roles, got %+v (err=%v)", nobody.Roles(), err)
	}
}

func TestSetUserRoles_AddsAndRemoves(t *testing.T) {
	tx := openTestDB(t)
	role := models.Role{Name: "Deployers"}
	if err := gorm.G[models.Role](tx).Create(t.Context(), &role); err != nil {
		t.Fatalf("failed to create role: %v", err)
	}
	createUser(t, tx, "alice", UserRoleID)

	changed, err := SetUserRoles(t.Context(), tx, "alice", models.UserRoleSourceManual, []string{role.Id, UserRoleID})
	if err != nil || !changed {
		t.Fatalf("expected a role to be added, changed=%v err=%v", changed, err)
	}
	changed, err = SetUserRoles(t.Context(), tx, "alice", models.UserRoleSourceManual, []string{role.Id})
	if err != nil || !changed {
		t.Fatalf("expected a role to be removed, changed=%v err=%v", changed, err)
	}

	assignments, err := gorm.G[models.UserRole](tx).Where("user_id = ?", "alice").Find(t.Context())
	if err != nil {
		t.Fatalf("failed to load roles: %v", err)
	}
	if len(assignments) != 1 || assignments[0].RoleId != role.Id {
		t.Fatalf("expected only the custom role, got %+v", assignments)
	}
}

func TestSetRoleMembers(t *testing.T) {
	tx := openTestDB(t)
	createUser(t, tx, "alice", UserRoleID)
	createUser(t, tx, "bob")
	createUser(t, tx, "carol")
	if _, err := SetUserRoles(t.Context(), tx, "carol", models.UserRoleSourceOIDC, []string{UserRoleID}); err != nil {
		t.Fatalf("SetUserRoles() error: %v", err)
	}

	changed, err := SetRoleMembers(t.Context(), tx, UserRoleID, models.UserRoleSourceManual, []string{"bob"})
	if err != nil || !changed {
		t.Fatalf("expected the members to change, changed=%v err=%v", changed, err)
	}
	changed, err = SetRoleMembers(t.Context(), tx, UserRoleID, models.UserRoleSourceManual, []string{"bob"})
	if err != nil || changed {
		t.Fatalf("expected no change, changed=%v err=%v", changed, err)
	}

	assignments, err := gorm.G[models.UserRole](tx).Where("role_id = ?", UserRoleID).Order("user_id").Find(t.Context())
	if err != nil {
		t.Fatalf("failed to load members: %v", err)
	}
	got := make([]string, 0, len(assignments))
	for i := range assignments {
		got = append(got, assignments[i].UserId+":"+string(assignments[i].Source))
	}
	// Members from OIDC groups are kept
	if want := []string{"bob:manual", "carol:oidc"}; !slices.Equal(got, want) {
		t.Fatalf("expected members %v, got %v", want, got)
	}
}

func TestAssignBuiltInRole(t *testing.T) {
	tx := openTestDB(t)
	createUser(t, tx, "alice")

	for range 2 {
		if err := AssignBuiltInRole(t.Context(), tx, "alice", models.RoleBuiltInAdmin); err != nil {
			t.Fatalf("AssignBuiltInRole() error: %v", err)
		}
	}
	count, err := CountAdmins(t.Context(), tx)
	if err != nil || count != 1 {
		t.Fatalf("expected alice to be the only admin, got %d (err=%v)", count, err)
	}
	if _, err := BuiltInRole(t.Context(), tx, "missing"); err == nil {
		t.Fatal("expected an error for an unknown built-in role")
	}
}

// createOIDCTables creates the provider and identity columns the sync reads,
// without the encrypted provider settings.
func createOIDCTables(t *testing.T, tx *gorm.DB) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE oidc_providers (id TEXT PRIMARY KEY, enabled INTEGER NOT NULL, groups_claim TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE user_oidc_identities (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, provider_id TEXT NOT NULL, group_names TEXT NOT NULL DEFAULT '[]')`,
		`INSERT INTO oidc_providers (id, enabled, groups_claim) VALUES ('corp', 1, 'groups'), ('git', 1, 'groups'), ('off', 1, '')`,
	} {
		if err := tx.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to create oidc tables: %v", err)
		}
	}
}

// setGroups sets the groups alice has at the provider.
func setGroups(t *testing.T, tx *gorm.DB, providerID, groups string) {
	t.Helper()
	if err := tx.Exec(`INSERT INTO user_oidc_identities (id, user_id, provider_id, group_names) VALUES (?, 'alice', ?, ?)
		ON CONFLICT(id) DO UPDATE SET group_names = excluded.group_names`, "alice-"+providerID, providerID, groups).Error; err != nil {
		t.Fatalf("failed to set groups: %v", err)
	}
}

func TestSyncOIDCRoles(t *testing.T) {
	tx := openTestDB(t)
	createOIDCTables(t, tx)
	devs := models.Role{Name: "Devs"}
	ops := models.Role{Name: "Ops"}
	for _, role := range []*models.Role{&devs, &ops} {
		if err := gorm.G[models.Role](tx).Create(t.Context(), role); err != nil {
			t.Fatalf("failed to create role: %v", err)
		}
	}
	mappings := []models.RoleOIDCGroup{
		{RoleId: devs.Id, ProviderId: "corp", Group: "devs"},
		{RoleId: devs.Id, ProviderId: "corp", Group: "engineering"},
		{RoleId: ops.Id, ProviderId: "corp", Group: "ops"},
		{RoleId: AdminRoleID, ProviderId: "corp", Group: "admins"},
		{RoleId: ops.Id, ProviderId: "off", Group: "ops"},
	}
	if err := gorm.G[models.RoleOIDCGroup](tx).CreateInBatches(t.Context(), &mappings, 10); err != nil {
		t.Fatalf("failed to map groups: %v", err)
	}
	createUser(t, tx, "alice", UserRoleID)

	rolesOf := func(source models.UserRoleSource) []string {
		t.Helper()
		assignments, err := gorm.G[models.UserRole](tx).Where("user_id = ? AND source = ?", "alice", source).Find(t.Context())
		if err != nil {
			t.Fatalf("failed to load roles: %v", err)
		}
		ids := []string{}
		for _, a := range assignments {
			ids = append(ids, a.RoleId)
		}
		slices.Sort(ids)
		return ids
	}
	sorted := func(ids ...string) []string { slices.Sort(ids); return ids }
	sync := func() []string {
		t.Helper()
		changed, err := SyncOIDCRoles(t.Context(), tx, []string{"alice"})
		if err != nil {
			t.Fatalf("SyncOIDCRoles() error: %v", err)
		}
		return changed
	}

	setGroups(t, tx, "corp", `["engineering", "ops", "unknown"]`)
	if changed := sync(); !slices.Equal(changed, []string{"alice"}) {
		t.Fatalf("expected alice's roles to change, got %v", changed)
	}
	if got := rolesOf(models.UserRoleSourceOIDC); !slices.Equal(got, sorted(devs.Id, ops.Id)) {
		t.Fatalf("expected devs and ops, got %v", got)
	}
	if changed := sync(); len(changed) != 0 {
		t.Fatalf("expected no change, got %v", changed)
	}

	// The same group names at another provider, or one that doesn't sync groups, map to nothing.
	setGroups(t, tx, "corp", `[]`)
	setGroups(t, tx, "git", `["admins", "devs"]`)
	setGroups(t, tx, "off", `["ops"]`)
	sync()
	if got := rolesOf(models.UserRoleSourceOIDC); len(got) != 0 {
		t.Fatalf("expected the oidc roles to be removed, got %v", got)
	}
	if got := rolesOf(models.UserRoleSourceManual); !slices.Equal(got, []string{UserRoleID}) {
		t.Fatalf("expected the manual role to stay, got %v", got)
	}

	// Alice becomes the only admin through her group and keeps the role after leaving it.
	setGroups(t, tx, "corp", `["admins"]`)
	sync()
	setGroups(t, tx, "corp", `[]`)
	if changed := sync(); len(changed) != 0 {
		t.Fatalf("expected the last admin to keep the role without changes, got %v", changed)
	}
	if p, err := Load(t.Context(), tx, "alice"); err != nil || !p.IsAdmin() {
		t.Fatalf("expected the last admin to keep the admin role, err=%v", err)
	}

	// With another admin around, leaving the group removes it.
	createUser(t, tx, "bob", AdminRoleID)
	sync()
	if p, err := Load(t.Context(), tx, "alice"); err != nil || p.IsAdmin() {
		t.Fatalf("expected alice to lose the admin role, err=%v", err)
	}

	// A disabled provider grants nothing.
	setGroups(t, tx, "corp", `["ops"]`)
	if err := tx.Exec(`UPDATE oidc_providers SET enabled = 0 WHERE id = 'corp'`).Error; err != nil {
		t.Fatalf("failed to disable provider: %v", err)
	}
	sync()
	if got := rolesOf(models.UserRoleSourceOIDC); len(got) != 0 {
		t.Fatalf("expected no roles from a disabled provider, got %v", got)
	}

	ids, err := OIDCUserIDs(t.Context(), tx)
	if err != nil || !slices.Contains(ids, "alice") {
		t.Fatalf("expected alice to be synced after mapping changes, got %v (err=%v)", ids, err)
	}
}
