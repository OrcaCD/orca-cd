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
	admins, err := CountAdmins(ctx, tx)
	if err != nil {
		return nil, err
	}
	adminSources, err := adminSourcesOf(ctx, tx, userIDs, admin.Id)
	if err != nil {
		return nil, err
	}

	var changed []string
	for _, userID := range userIDs {
		wanted := roleIDs[userID]
		sources := adminSources[userID]
		switch {
		case slices.Contains(wanted, admin.Id):
			if len(sources) == 0 {
				admins++
			}
		// Users with only an OIDC admin assignment lose the admin role
		case slices.Equal(sources, []models.UserRoleSource{models.UserRoleSourceOIDC}):
			if admins == 1 {
				oidcLog.Warn().Str("userId", userID).Msg("keeping the admin role of a user who left its OIDC group, since they are the last admin")
				wanted = append(wanted, admin.Id)
			} else {
				admins--
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

// adminSourcesOf returns the sources of the admin role of the users.
func adminSourcesOf(ctx context.Context, tx *gorm.DB, userIDs []string, adminID string) (map[string][]models.UserRoleSource, error) {
	assignments, err := gorm.G[models.UserRole](tx).Where("role_id = ? AND user_id IN ?", adminID, userIDs).Find(ctx)
	if err != nil {
		return nil, err
	}
	sources := make(map[string][]models.UserRoleSource, len(assignments))
	for _, assignment := range assignments {
		sources[assignment.UserId] = append(sources[assignment.UserId], assignment.Source)
	}
	return sources, nil
}
