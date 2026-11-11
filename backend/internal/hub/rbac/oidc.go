package rbac

import (
	"context"
	"slices"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"gorm.io/gorm"
)

var oidcLog = logger.New("rbac", false)

type oidcRoleRow struct {
	UserId string
	RoleId string
}

// oidcRoleIDs returns the roles the users get from their groups at enabled OIDC
// providers that sync groups, as of their last login there.
func oidcRoleIDs(ctx context.Context, tx *gorm.DB, userIDs []string) (map[string][]string, error) {
	rows, err := gorm.G[oidcRoleRow](tx).Raw(`
		SELECT DISTINCT uoi.user_id, rog.role_id
		FROM user_oidc_identities uoi
		JOIN oidc_providers op ON op.id = uoi.provider_id AND op.enabled = 1 AND op.groups_claim <> ''
		JOIN json_each(uoi.group_names) g
		JOIN role_oidc_groups rog ON rog.provider_id = uoi.provider_id AND rog.group_name = g.value
		WHERE uoi.user_id IN ?`, userIDs).Find(ctx)
	if err != nil {
		return nil, err
	}
	roleIDs := make(map[string][]string, len(userIDs))
	for _, row := range rows {
		roleIDs[row.UserId] = append(roleIDs[row.UserId], row.RoleId)
	}
	return roleIDs, nil
}

// OIDCUserIDs returns the users whose roles can depend on OIDC groups.
func OIDCUserIDs(ctx context.Context, tx *gorm.DB) ([]string, error) {
	var ids []string
	err := tx.WithContext(ctx).Raw(`
		SELECT user_id FROM user_oidc_identities WHERE group_names <> '[]'
		UNION
		SELECT user_id FROM user_roles WHERE source = ?`, models.UserRoleSourceOIDC).Scan(&ids).Error
	return ids, err
}

// SyncOIDCRoles sets the roles the users get from their groups at OIDC providers.
// The admin role is kept when losing it would leave the hub without an admin.
// Returns the users whose roles changed.
func SyncOIDCRoles(ctx context.Context, tx *gorm.DB, userIDs []string) ([]string, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	roleIDs, err := oidcRoleIDs(ctx, tx, userIDs)
	if err != nil {
		return nil, err
	}
	admin, err := BuiltInRole(ctx, tx, models.RoleBuiltInAdmin)
	if err != nil {
		return nil, err
	}

	var changed []string
	for _, userID := range userIDs {
		wanted := roleIDs[userID]
		if !slices.Contains(wanted, admin.Id) {
			keep, err := isLastAdminFromOIDC(ctx, tx, userID, admin.Id)
			if err != nil {
				return nil, err
			}
			if keep {
				oidcLog.Warn().Str("userId", userID).Msg("keeping the admin role of a user who left its OIDC group, since they are the last admin")
				wanted = append(wanted, admin.Id)
			}
		}
		userChanged, err := SetUserRoles(ctx, tx, userID, models.UserRoleSourceOIDC, wanted)
		if err != nil {
			return nil, err
		}
		if userChanged {
			changed = append(changed, userID)
		}
	}
	return changed, nil
}

// isLastAdminFromOIDC reports whether the user is the only admin and only got
// the admin role from an OIDC group.
func isLastAdminFromOIDC(ctx context.Context, tx *gorm.DB, userID, adminID string) (bool, error) {
	assignments, err := gorm.G[models.UserRole](tx).Where("user_id = ? AND role_id = ?", userID, adminID).Find(ctx)
	if err != nil {
		return false, err
	}
	if len(assignments) != 1 || assignments[0].Source != models.UserRoleSourceOIDC {
		return false, nil
	}
	admins, err := CountAdmins(ctx, tx)
	return admins == 1, err
}
