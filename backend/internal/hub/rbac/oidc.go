package rbac

import (
	"context"
	"slices"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
)

// OIDCRoles returns the roles mapped to any of the groups.
func OIDCRoles(ctx context.Context, tx *gorm.DB, groups []string) ([]models.Role, error) {
	if len(groups) == 0 {
		return []models.Role{}, nil
	}
	roles, err := gorm.G[models.Role](tx).Where("oidc_groups <> '[]'").Find(ctx)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(roles, func(role models.Role) bool {
		return !slices.ContainsFunc(role.OIDCGroups, func(group string) bool { return slices.Contains(groups, group) })
	}), nil
}

// SyncOIDCRoles replaces the roles a user got from OIDC groups with the ones
// mapped to their current groups. The admin role is kept when losing it would
// leave the hub without an admin. Returns whether anything changed.
func SyncOIDCRoles(ctx context.Context, tx *gorm.DB, userID string, groups []string) (bool, error) {
	roles, err := OIDCRoles(ctx, tx, groups)
	if err != nil {
		return false, err
	}
	roleIDs := make([]string, 0, len(roles))
	for i := range roles {
		roleIDs = append(roleIDs, roles[i].Id)
	}

	before, err := CountAdmins(ctx, tx)
	if err != nil {
		return false, err
	}
	changed, err := SetUserRoles(ctx, tx, userID, models.UserRoleSourceOIDC, roleIDs)
	if err != nil {
		return false, err
	}
	after, err := CountAdmins(ctx, tx)
	if err != nil {
		return false, err
	}
	if before > 0 && after == 0 {
		admin, err := BuiltInRole(ctx, tx, models.RoleBuiltInAdmin)
		if err != nil {
			return false, err
		}
		if _, err := SetUserRoles(ctx, tx, userID, models.UserRoleSourceOIDC, append(roleIDs, admin.Id)); err != nil {
			return false, err
		}
	}
	return changed, nil
}
