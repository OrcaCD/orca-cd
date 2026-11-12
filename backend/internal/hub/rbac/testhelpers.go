package rbac

import (
	"context"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Fixed IDs of the built-in roles, as created by migration 000041.
const (
	AdminRoleID = "00000000-0000-7000-8000-000000000001"
	UserRoleID  = "00000000-0000-7000-8000-000000000002"
)

// MigrateForTest creates the role tables on a database that was set up with
// AutoMigrate and seeds the built-in roles like the SQL migrations do.
func MigrateForTest(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&models.Role{}, &models.RolePermission{}, &models.UserRole{}, &models.RoleOIDCGroup{}); err != nil {
		return err
	}
	// AutoMigrate cannot create these indexes from model tags.
	for _, stmt := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_name ON roles(name COLLATE NOCASE)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_role_permissions_unique ON role_permissions(role_id, resource, action, IFNULL(resource_id, ''))`,
	} {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}

	ctx := context.Background()
	admin, user := models.RoleBuiltInAdmin, models.RoleBuiltInUser
	roles := []models.Role{
		{Id: AdminRoleID, BuiltIn: &admin, Name: "Admin"},
		{Id: UserRoleID, BuiltIn: &user, Name: "User"},
	}
	for i := range roles {
		if err := gorm.G[models.Role](tx, clause.OnConflict{DoNothing: true}).Create(ctx, &roles[i]); err != nil {
			return err
		}
	}

	count, err := gorm.G[models.RolePermission](tx).Where("role_id = ?", UserRoleID).Count(ctx, "*")
	if err != nil || count > 0 {
		return err
	}
	for _, resource := range models.PermissionResources {
		for _, action := range models.PermissionActions[resource] {
			if err := gorm.G[models.RolePermission](tx).Create(ctx, &models.RolePermission{
				RoleId:   UserRoleID,
				Resource: resource,
				Action:   action,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
