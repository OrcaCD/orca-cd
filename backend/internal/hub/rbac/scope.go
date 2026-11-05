package rbac

import (
	"context"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Scope limits a query to the resources the action is allowed on.
func (p *Permissions) Scope(resource models.PermissionResource, action models.PermissionAction, column string) func(*gorm.Statement) {
	ids, all := p.IDs(resource, action)
	return func(stmt *gorm.Statement) {
		if all {
			return
		}
		values := make([]any, len(ids))
		for i, id := range ids {
			values[i] = id
		}
		// An empty IN matches nothing.
		stmt.AddClause(clause.Where{Exprs: []clause.Expression{
			clause.IN{Column: clause.Column{Table: clause.CurrentTable, Name: column}, Values: values},
		}})
	}
}

// resourceTables maps the resource types to the tables holding them.
var resourceTables = map[models.PermissionResource]string{
	models.ResourceApplication:  "applications",
	models.ResourceRepository:   "repositories",
	models.ResourceAgent:        "agents",
	models.ResourceNotification: "notifications",
}

// PruneGrants removes grants on single resources that no longer exist. Grants
// cannot reference the resources with a foreign key, so this runs after deletes,
// including the ones that cascade, like the applications of a deleted repository.
// Returns how many grants were removed.
func PruneGrants(ctx context.Context, tx *gorm.DB) (int, error) {
	removed := 0
	for _, resource := range models.PermissionResources {
		rows, err := gorm.G[models.RolePermission](tx).
			Where("resource = ? AND resource_id IS NOT NULL AND resource_id NOT IN (SELECT id FROM "+resourceTables[resource]+")", resource).
			Delete(ctx)
		if err != nil {
			return removed, err
		}
		removed += rows
	}
	return removed, nil
}
