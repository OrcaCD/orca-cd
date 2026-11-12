package rbac

import (
	"context"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BuiltInRole loads one of the roles every installation has.
func BuiltInRole(ctx context.Context, tx *gorm.DB, builtIn models.RoleBuiltIn) (models.Role, error) {
	return gorm.G[models.Role](tx).Where("built_in = ?", builtIn).First(ctx)
}

// AssignBuiltInRole gives a user one of the built-in roles by hand.
func AssignBuiltInRole(ctx context.Context, tx *gorm.DB, userID string, builtIn models.RoleBuiltIn) error {
	role, err := BuiltInRole(ctx, tx, builtIn)
	if err != nil {
		return err
	}
	return gorm.G[models.UserRole](tx, clause.OnConflict{DoNothing: true}).Create(ctx, &models.UserRole{
		UserId: userID,
		RoleId: role.Id,
		Source: models.UserRoleSourceManual,
	})
}

// adminUserIDs selects the users that have the admin role.
func adminUserIDs(tx *gorm.DB) *gorm.DB {
	return tx.Model(&models.UserRole{}).
		Select("user_roles.user_id").
		Joins("JOIN roles ON roles.id = user_roles.role_id").
		Where("roles.built_in = ?", models.RoleBuiltInAdmin)
}

// CountAdmins returns how many users have the admin role.
func CountAdmins(ctx context.Context, tx *gorm.DB) (int64, error) {
	return gorm.G[models.User](tx).Where("id IN (?)", adminUserIDs(tx.WithContext(ctx))).Count(ctx, "*")
}

// MemberIDs returns the users that have any of the roles, from any source.
func MemberIDs(ctx context.Context, tx *gorm.DB, roleIDs []string) ([]string, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	var ids []string
	err := tx.WithContext(ctx).Model(&models.UserRole{}).
		Where("role_id IN ?", roleIDs).
		Distinct().
		Pluck("user_id", &ids).Error
	return ids, err
}

// SetUserRoles replaces the roles a user got from the given source. Roles from
// other sources are kept. Returns whether anything changed.
func SetUserRoles(ctx context.Context, tx *gorm.DB, userID string, source models.UserRoleSource, roleIDs []string) (bool, error) {
	current, err := gorm.G[models.UserRole](tx).Where("user_id = ? AND source = ?", userID, source).Find(ctx)
	if err != nil {
		return false, err
	}

	wanted := make(map[string]struct{}, len(roleIDs))
	for _, id := range roleIDs {
		wanted[id] = struct{}{}
	}

	changed := false
	for i := range current {
		if _, ok := wanted[current[i].RoleId]; ok {
			delete(wanted, current[i].RoleId)
			continue
		}
		if _, err := gorm.G[models.UserRole](tx).
			Where("user_id = ? AND role_id = ? AND source = ?", userID, current[i].RoleId, source).
			Delete(ctx); err != nil {
			return false, err
		}
		changed = true
	}

	for id := range wanted {
		if err := gorm.G[models.UserRole](tx).Create(ctx, &models.UserRole{UserId: userID, RoleId: id, Source: source}); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

// SetRoleMembers replaces the users that got the role from the given source.
// Returns whether anything changed.
func SetRoleMembers(ctx context.Context, tx *gorm.DB, roleID string, source models.UserRoleSource, userIDs []string) (bool, error) {
	current, err := gorm.G[models.UserRole](tx).Where("role_id = ? AND source = ?", roleID, source).Find(ctx)
	if err != nil {
		return false, err
	}

	wanted := make(map[string]struct{}, len(userIDs))
	for _, id := range userIDs {
		wanted[id] = struct{}{}
	}

	changed := false
	for i := range current {
		if _, ok := wanted[current[i].UserId]; ok {
			delete(wanted, current[i].UserId)
			continue
		}
		if _, err := gorm.G[models.UserRole](tx).
			Where("user_id = ? AND role_id = ? AND source = ?", current[i].UserId, roleID, source).
			Delete(ctx); err != nil {
			return false, err
		}
		changed = true
	}

	for id := range wanted {
		if err := gorm.G[models.UserRole](tx).Create(ctx, &models.UserRole{UserId: id, RoleId: roleID, Source: source}); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}
