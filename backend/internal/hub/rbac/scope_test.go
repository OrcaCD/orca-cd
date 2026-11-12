package rbac

import (
	"slices"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
)

type scopedApplication struct{ Id string }

func (scopedApplication) TableName() string { return "applications" }

// createResourceTables creates minimal tables for the resources grants can
// reference, holding the given IDs.
func createResourceTables(t *testing.T, tx *gorm.DB, ids map[string][]string) {
	t.Helper()
	for _, table := range resourceTables {
		if err := tx.Exec("CREATE TABLE " + table + " (id TEXT PRIMARY KEY)").Error; err != nil {
			t.Fatalf("failed to create %s: %v", table, err)
		}
		for _, id := range ids[table] {
			if err := tx.Exec("INSERT INTO "+table+" (id) VALUES (?)", id).Error; err != nil {
				t.Fatalf("failed to insert into %s: %v", table, err)
			}
		}
	}
}

func TestScope(t *testing.T) {
	tx := openTestDB(t)
	createResourceTables(t, tx, map[string][]string{"applications": {"app-1", "app-2", "app-3"}})
	app1, app3 := "app-1", "app-3"

	tests := []struct {
		name  string
		perms *Permissions
		want  []string
	}{
		{"admin", Admin(), []string{"app-1", "app-2", "app-3"}},
		{"global grant", New([]models.RolePermission{grant(models.ResourceApplication, models.ActionRead, nil)}), []string{"app-1", "app-2", "app-3"}},
		{"scoped grants", New([]models.RolePermission{
			grant(models.ResourceApplication, models.ActionRead, &app1),
			grant(models.ResourceApplication, models.ActionDeploy, &app3),
		}), []string{"app-1", "app-3"}},
		{"grant on another resource", New([]models.RolePermission{grant(models.ResourceAgent, models.ActionRead, nil)}), []string{}},
		{"no grants", New(nil), []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apps, err := gorm.G[scopedApplication](tx).
				Scopes(tt.perms.Scope(models.ResourceApplication, models.ActionRead, "id")).
				Order("id").
				Find(t.Context())
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			got := make([]string, 0, len(apps))
			for _, app := range apps {
				got = append(got, app.Id)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestPruneGrants(t *testing.T) {
	tx := openTestDB(t)
	createResourceTables(t, tx, map[string][]string{"applications": {"app-1"}, "agents": {"agent-1"}})

	role := models.Role{Name: "Scoped"}
	if err := gorm.G[models.Role](tx).Create(t.Context(), &role); err != nil {
		t.Fatalf("failed to create role: %v", err)
	}
	ids := []string{"app-1", "deleted-app", "agent-1", "deleted-agent"}
	grants := []models.RolePermission{
		grant(models.ResourceApplication, models.ActionRead, &ids[0]),
		grant(models.ResourceApplication, models.ActionRead, &ids[1]),
		grant(models.ResourceAgent, models.ActionUpdate, &ids[2]),
		grant(models.ResourceAgent, models.ActionUpdate, &ids[3]),
		grant(models.ResourceRepository, models.ActionRead, nil),
	}
	for i := range grants {
		grants[i].RoleId = role.Id
	}
	if err := gorm.G[models.RolePermission](tx).CreateInBatches(t.Context(), &grants, 10); err != nil {
		t.Fatalf("failed to create grants: %v", err)
	}

	removed, err := PruneGrants(t.Context(), tx)
	if err != nil || removed != 2 {
		t.Fatalf("PruneGrants() = %d, %v; want 2 removed grants", removed, err)
	}

	remaining, err := gorm.G[models.RolePermission](tx).Where("role_id = ?", role.Id).Find(t.Context())
	if err != nil {
		t.Fatalf("failed to load grants: %v", err)
	}
	var kept []string
	for _, g := range remaining {
		id := "*"
		if g.ResourceId != nil {
			id = *g.ResourceId
		}
		kept = append(kept, string(g.Resource)+":"+id)
	}
	slices.Sort(kept)
	want := []string{"agent:agent-1", "application:app-1", "repository:*"}
	if !slices.Equal(kept, want) {
		t.Fatalf("expected %v to remain, got %v", want, kept)
	}

	if removed, err := PruneGrants(t.Context(), tx); err != nil || removed != 0 {
		t.Fatalf("second PruneGrants() = %d, %v; want nothing removed", removed, err)
	}
}
