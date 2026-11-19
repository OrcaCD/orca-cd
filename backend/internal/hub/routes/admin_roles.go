package routes

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

type rolePermissionRequest struct {
	// Not an enum, so unknown values keep answering "invalid permission"
	Resource   models.PermissionResource `json:"resource" required:"true" minLength:"1" doc:"application, repository, agent, notification or api_token"`
	Action     models.PermissionAction   `json:"action" required:"true" minLength:"1" doc:"An action the resource supports, like read, create, update, delete, deploy, pull_images, rollback or sync"`
	ResourceId *string                   `json:"resourceId" doc:"Limits the permission to one resource. Leave it out or null to grant it on all of them. Not allowed for create."`
}

type upsertRoleRequest struct {
	Name        string                  `json:"name" required:"true" minLength:"1" maxLength:"64" doc:"Ignored for built-in roles"`
	Description string                  `json:"description" maxLength:"256" doc:"Ignored for built-in roles"`
	Permissions []rolePermissionRequest `json:"permissions" maxItems:"5000" doc:"Replaces the permissions of the role. Ignored for the admin role, which can do everything."`
	// OIDCGroups give the role to users in any of these groups at their OIDC provider.
	OIDCGroups []roleOIDCGroup `json:"oidcGroups" maxItems:"100" doc:"Gives the role to the users in any of these groups at their OIDC provider"`
	// UserIds are the members an admin assigned by hand. Members from OIDC groups are not affected.
	UserIds []string `json:"userIds" maxItems:"10000" doc:"The members an admin assigns. Members from OIDC groups are not affected."`
}

type roleOIDCGroup struct {
	ProviderId string `json:"providerId" required:"true" minLength:"1"`
	Group      string `json:"group" required:"true" minLength:"1" maxLength:"255"`
}

type rolePermissionResponse struct {
	Resource   models.PermissionResource `json:"resource" required:"true" enum:"application,repository,agent,notification,api_token"`
	Action     models.PermissionAction   `json:"action" required:"true" enum:"read,create,update,delete,deploy,pull_images,rollback,sync"`
	ResourceId *string                   `json:"resourceId" required:"true" nullable:"true" doc:"The resource the permission is limited to, null for all of them"`
}

type roleMemberResponse struct {
	Id      string                  `json:"id" required:"true"`
	Name    string                  `json:"name" required:"true"`
	Email   string                  `json:"email" required:"true"`
	Sources []models.UserRoleSource `json:"sources" required:"true" enum:"manual,oidc" doc:"Whether an admin assigned the role, the user gets it from an OIDC group, or both"`
}

type roleResponse struct {
	Id          string                   `json:"id" required:"true"`
	Name        string                   `json:"name" required:"true"`
	Description string                   `json:"description" required:"true"`
	BuiltIn     *models.RoleBuiltIn      `json:"builtIn" required:"true" nullable:"true" enum:"admin,user" doc:"Which built-in role it is, null for roles admins created"`
	OIDCGroups  []roleOIDCGroup          `json:"oidcGroups" required:"true"`
	Permissions []rolePermissionResponse `json:"permissions" required:"true"`
	Members     []roleMemberResponse     `json:"members" required:"true"`
	CreatedAt   string                   `json:"createdAt" required:"true" format:"date-time"`
	UpdatedAt   string                   `json:"updatedAt" required:"true" format:"date-time"`
}

type roleOutput struct {
	Body roleResponse
}

var errRoleNotFound = errors.New("role not found")

var AdminListRolesHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-list-roles",
	Method:      http.MethodGet,
	Path:        "/admin/roles",
	Summary:     "List roles",
	Description: "Lists every role with its permissions, OIDC groups and members, the built-in roles first. " + requiresAdmin,
	Tags:        []string{tagAdmin},
	Security:    restapi.Authenticated,
}, func(ctx context.Context, _ *struct{}) (*struct{ Body []roleResponse }, error) {
	roles, err := gorm.G[models.Role](db.DB).Preload("Permissions", nil).Preload("OIDCGroups", nil).Find(ctx)
	if err != nil {
		return nil, internalError()
	}
	slices.SortFunc(roles, compareRoles)

	roleIds := make([]string, 0, len(roles))
	for i := range roles {
		roleIds = append(roleIds, roles[i].Id)
	}
	membersByRoleId, err := loadMembersByRoleId(ctx, roleIds)
	if err != nil {
		return nil, internalError()
	}

	response := make([]roleResponse, 0, len(roles))
	for i := range roles {
		response = append(response, toRoleResponse(&roles[i], membersByRoleId[roles[i].Id]))
	}

	return &struct{ Body []roleResponse }{Body: response}, nil
})

var AdminGetRoleHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-get-role",
	Method:      http.MethodGet,
	Path:        "/admin/roles/{id}",
	Summary:     "Get a role",
	Description: requiresAdmin,
	Tags:        []string{tagAdmin},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusNotFound},
}, func(ctx context.Context, input *idInput) (*roleOutput, error) {
	role, err := loadRole(ctx, input.ID)
	if err != nil {
		return nil, roleError(err)
	}

	return roleOutputOf(ctx, &role)
})

var AdminCreateRoleHandler = restapi.Handler(huma.Operation{
	OperationID:   "admin-create-role",
	Method:        http.MethodPost,
	Path:          "/admin/roles",
	DefaultStatus: http.StatusCreated,
	Summary:       "Create a role",
	Description:   "The resources and users the role refers to must exist. " + requiresAdmin,
	Tags:          []string{tagAdmin},
	Security:      restapi.Authenticated,
	Errors:        []int{http.StatusBadRequest, http.StatusConflict},
}, func(ctx context.Context, input *struct{ Body upsertRoleRequest }) (*roleOutput, error) {
	req, permissions, err := normalizeRoleRequest(input.Body)
	if err != nil {
		return nil, err
	}

	role := models.Role{
		Name:        req.Name,
		Description: req.Description,
	}

	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkRoleReferences(ctx, tx, "", permissions, req.UserIds); err != nil {
			return err
		}
		if err := gorm.G[models.Role](tx).Create(ctx, &role); err != nil {
			return err
		}
		if err := replaceRolePermissions(ctx, tx, role.Id, permissions); err != nil {
			return err
		}
		if _, err := replaceRoleOIDCGroups(ctx, tx, role.Id, req.OIDCGroups); err != nil {
			return err
		}
		_, err := rbac.SetRoleMembers(ctx, tx, role.Id, models.UserRoleSourceManual, req.UserIds)
		return err
	})
	if err != nil {
		return nil, roleError(err)
	}

	role, err = loadRole(ctx, role.Id)
	if err != nil {
		return nil, roleError(err)
	}

	utils.RecordAuditLog(restapi.GinContext(ctx), "created", "role", role.Id)

	out, err := roleOutputOf(ctx, &role)
	if err != nil {
		return nil, err
	}
	publishRoleUpdate(req.UserIds)
	return out, nil
})

var AdminUpdateRoleHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-update-role",
	Method:      http.MethodPut,
	Path:        "/admin/roles/{id}",
	Summary:     "Update a role",
	Description: "Replaces the permissions, OIDC groups and members an admin assigned. The name and description of built-in roles " +
		"and the permissions of the admin role don't change, and the last admin can't lose the admin role. " + requiresAdmin,
	Tags:     []string{tagAdmin},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict},
}, func(ctx context.Context, input *struct {
	ID   string `path:"id" doc:"The ID of the role"`
	Body upsertRoleRequest
}) (*roleOutput, error) {
	req, permissions, err := normalizeRoleRequest(input.Body)
	if err != nil {
		return nil, err
	}

	id := input.ID
	membersChanged := false
	// Everyone who had or has the role, their permissions may have changed
	var affected []string

	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		role, err := gorm.G[models.Role](tx).Where("id = ?", id).First(ctx)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errRoleNotFound
			}
			return err
		}
		if err := checkRoleReferences(ctx, tx, id, permissions, req.UserIds); err != nil {
			return err
		}
		if affected, err = rbac.MemberIDs(ctx, tx, []string{id}); err != nil {
			return err
		}

		// The names of the built-in roles are translated by clients, and the
		// permissions of the admin role are implicit.
		columns := []any{}
		if role.BuiltIn == nil {
			columns = append(columns, "name", "description")
		}
		if _, err := gorm.G[models.Role](tx).Where("id = ?", id).
			Select("updated_at", columns...).
			Updates(ctx, models.Role{Name: req.Name, Description: req.Description, UpdatedAt: time.Now()}); err != nil {
			return err
		}
		if !role.IsBuiltIn(models.RoleBuiltInAdmin) {
			if err := replaceRolePermissions(ctx, tx, id, permissions); err != nil {
				return err
			}
		}

		err = keepLastAdmin(ctx, tx, func() error {
			groupsChanged, err := replaceRoleOIDCGroups(ctx, tx, id, req.OIDCGroups)
			if err != nil {
				return err
			}
			// Members from groups follow the new groups right away
			if groupsChanged {
				synced, err := syncAllOIDCRoles(ctx, tx)
				if err != nil {
					return err
				}
				affected = append(affected, synced...)
			}
			membersChanged, err = rbac.SetRoleMembers(ctx, tx, id, models.UserRoleSourceManual, req.UserIds)
			return err
		})
		if err != nil {
			return err
		}
		members, err := rbac.MemberIDs(ctx, tx, []string{id})
		affected = append(affected, members...)
		return err
	})
	if err != nil {
		if errors.Is(err, errLastAdmin) {
			return nil, huma.Error400BadRequest("cannot remove the last admin user")
		}
		return nil, roleError(err)
	}

	role, err := loadRole(ctx, id)
	if err != nil {
		return nil, roleError(err)
	}

	c := restapi.GinContext(ctx)
	utils.RecordAuditLog(c, "updated", "role", id)
	if membersChanged {
		utils.RecordAuditLog(c, "members-updated", "role", id)
	}

	out, err := roleOutputOf(ctx, &role)
	if err != nil {
		return nil, err
	}
	publishRoleUpdate(affected)
	return out, nil
})

var AdminDeleteRoleHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-delete-role",
	Method:      http.MethodDelete,
	Path:        "/admin/roles/{id}",
	Summary:     "Delete a role",
	Description: "Built-in roles can't be deleted. " + requiresAdmin,
	Tags:        []string{tagAdmin},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusBadRequest, http.StatusNotFound},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	id := input.ID
	var members []string

	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		role, err := gorm.G[models.Role](tx).Where("id = ?", id).First(ctx)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errRoleNotFound
			}
			return err
		}
		if role.BuiltIn != nil {
			return errBuiltInRole
		}
		if members, err = rbac.MemberIDs(ctx, tx, []string{id}); err != nil {
			return err
		}

		if _, err := gorm.G[models.UserRole](tx).Where("role_id = ?", id).Delete(ctx); err != nil {
			return err
		}
		if _, err := gorm.G[models.RolePermission](tx).Where("role_id = ?", id).Delete(ctx); err != nil {
			return err
		}
		_, err = gorm.G[models.Role](tx).Where("id = ?", id).Delete(ctx)
		return err
	})
	if err != nil {
		return nil, roleError(err)
	}

	utils.RecordAuditLog(restapi.GinContext(ctx), "deleted", "role", id)
	publishRoleUpdate(members)

	return restapi.Message("role deleted"), nil
})

var (
	errBuiltInRole       = errors.New("built-in roles cannot be deleted")
	errInvalidPermission = errors.New("invalid permission")
	errUnknownResource   = errors.New("permission refers to a resource that does not exist")
	errUnknownUser       = errors.New("user not found")
	errUnknownProvider   = errors.New("OIDC provider not found")
)

func roleError(err error) error {
	switch {
	case errors.Is(err, errRoleNotFound):
		return huma.Error404NotFound("role not found")
	case errors.Is(err, errBuiltInRole), errors.Is(err, errUnknownResource), errors.Is(err, errUnknownUser), errors.Is(err, errUnknownProvider):
		return huma.Error400BadRequest(err.Error())
	case isUniqueConstraintError(err):
		return huma.Error409Conflict("a role with this name already exists")
	default:
		return internalError()
	}
}

// normalizeRoleRequest trims and validates a role, returning its deduplicated
// permissions. Whether the referenced users and resources exist is checked by
// checkRoleReferences in the write transaction.
func normalizeRoleRequest(req upsertRoleRequest) (upsertRoleRequest, []models.RolePermission, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.OIDCGroups = normalizeOIDCGroups(req.OIDCGroups)
	if req.Name == "" {
		return req, nil, huma.Error400BadRequest("invalid request: name is required and must be at most 64 characters")
	}

	permissions, err := normalizeRolePermissions(req.Permissions)
	if err != nil {
		return req, nil, huma.Error400BadRequest(err.Error())
	}
	req.UserIds = uniqueIds(req.UserIds)

	return req, permissions, nil
}

func normalizeOIDCGroups(groups []roleOIDCGroup) []roleOIDCGroup {
	seen := make(map[roleOIDCGroup]struct{}, len(groups))
	unique := make([]roleOIDCGroup, 0, len(groups))
	for _, group := range groups {
		group.Group = strings.TrimSpace(group.Group)
		if _, ok := seen[group]; ok || group.Group == "" {
			continue
		}
		seen[group] = struct{}{}
		unique = append(unique, group)
	}
	return unique
}

// replaceRoleOIDCGroups sets the OIDC groups of a role. Returns whether they changed.
func replaceRoleOIDCGroups(ctx context.Context, tx *gorm.DB, roleId string, groups []roleOIDCGroup) (bool, error) {
	providerIds := make([]string, 0, len(groups))
	for _, group := range groups {
		providerIds = append(providerIds, group.ProviderId)
	}
	providerIds = uniqueIds(providerIds)
	if len(providerIds) > 0 {
		count, err := gorm.G[models.OIDCProvider](tx).Where("id IN ?", providerIds).Count(ctx, "*")
		if err != nil {
			return false, err
		}
		if int(count) != len(providerIds) {
			return false, errUnknownProvider
		}
	}

	current, err := gorm.G[models.RoleOIDCGroup](tx).Where("role_id = ?", roleId).Find(ctx)
	if err != nil {
		return false, err
	}
	wanted := make(map[roleOIDCGroup]struct{}, len(groups))
	for _, group := range groups {
		wanted[group] = struct{}{}
	}
	if len(current) == len(wanted) && !slices.ContainsFunc(current, func(g models.RoleOIDCGroup) bool {
		_, ok := wanted[roleOIDCGroup{ProviderId: g.ProviderId, Group: g.Group}]
		return !ok
	}) {
		return false, nil
	}

	if _, err := gorm.G[models.RoleOIDCGroup](tx).Where("role_id = ?", roleId).Delete(ctx); err != nil {
		return false, err
	}
	if len(groups) == 0 {
		return true, nil
	}
	rows := make([]models.RoleOIDCGroup, 0, len(groups))
	for _, group := range groups {
		rows = append(rows, models.RoleOIDCGroup{RoleId: roleId, ProviderId: group.ProviderId, Group: group.Group})
	}
	return true, gorm.G[models.RoleOIDCGroup](tx).CreateInBatches(ctx, &rows, 100)
}

// syncAllOIDCRoles updates the roles every user gets from OIDC groups, after the
// groups of a role or a provider changed. Returns the users whose roles changed.
func syncAllOIDCRoles(ctx context.Context, tx *gorm.DB) ([]string, error) {
	userIds, err := rbac.OIDCUserIDs(ctx, tx)
	if err != nil {
		return nil, err
	}
	return rbac.SyncOIDCRoles(ctx, tx, userIds)
}

// publishRolesChanged tells admins and the users whose roles changed, if any.
func publishRolesChanged(userIds []string) {
	if len(userIds) > 0 {
		publishRoleUpdate(userIds)
	}
}

// publishRoleUpdate tells admins that roles changed, and the given users to
// reload their permissions. Other users keep their permissions and get nothing.
func publishRoleUpdate(userIds []string) {
	topics := []sse.Topic{sse.TopicAdminUsers, sse.TopicAdminRoles}
	for _, id := range uniqueIds(userIds) {
		topics = append(topics, sse.TopicProfileOf(id))
	}
	sse.PublishUpdate(topics...)
}

func normalizeRolePermissions(items []rolePermissionRequest) ([]models.RolePermission, error) {
	type key struct {
		resource   models.PermissionResource
		action     models.PermissionAction
		resourceId string
	}

	seen := make(map[key]struct{}, len(items))
	permissions := make([]models.RolePermission, 0, len(items))

	for _, item := range items {
		if !models.IsValidPermission(item.Resource, item.Action) {
			return nil, errInvalidPermission
		}
		k := key{item.Resource, item.Action, ""}
		var resourceId *string
		if item.ResourceId != nil && *item.ResourceId != "" {
			if item.Action == models.ActionCreate {
				return nil, errInvalidPermission
			}
			resourceId = new(*item.ResourceId)
			k.resourceId = *resourceId
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}

		permissions = append(permissions, models.RolePermission{
			Resource:   item.Resource,
			Action:     item.Action,
			ResourceId: resourceId,
		})
	}

	return permissions, nil
}

// checkRoleReferences checks that the users and resources a role refers to
// exist. Resources the role already has grants on are not checked, so a role
// with a grant on a deleted resource can still be saved.
func checkRoleReferences(ctx context.Context, tx *gorm.DB, roleId string, permissions []models.RolePermission, userIds []string) error {
	type resourceKey struct {
		resource models.PermissionResource
		id       string
	}
	known := make(map[resourceKey]struct{})
	if roleId != "" {
		current, err := gorm.G[models.RolePermission](tx).Where("role_id = ? AND resource_id IS NOT NULL", roleId).Find(ctx)
		if err != nil {
			return err
		}
		for i := range current {
			known[resourceKey{current[i].Resource, *current[i].ResourceId}] = struct{}{}
		}
	}

	idsByResource := make(map[models.PermissionResource][]string)
	for i := range permissions {
		id := permissions[i].ResourceId
		if id == nil {
			continue
		}
		if _, ok := known[resourceKey{permissions[i].Resource, *id}]; ok {
			continue
		}
		idsByResource[permissions[i].Resource] = append(idsByResource[permissions[i].Resource], *id)
	}
	for resource, ids := range idsByResource {
		ids = uniqueIds(ids)
		count, err := countResources(ctx, tx, resource, ids)
		if err != nil {
			return err
		}
		if int(count) != len(ids) {
			return errUnknownResource
		}
	}

	if len(userIds) == 0 {
		return nil
	}
	count, err := gorm.G[models.User](tx).Where("id IN ?", userIds).Count(ctx, "*")
	if err != nil {
		return err
	}
	if int(count) != len(userIds) {
		return errUnknownUser
	}
	return nil
}

func countResources(ctx context.Context, tx *gorm.DB, resource models.PermissionResource, ids []string) (int64, error) {
	switch resource {
	case models.ResourceApplication:
		return gorm.G[models.Application](tx).Where("id IN ?", ids).Count(ctx, "*")
	case models.ResourceRepository:
		return gorm.G[models.Repository](tx).Where("id IN ?", ids).Count(ctx, "*")
	case models.ResourceAgent:
		return gorm.G[models.Agent](tx).Where("id IN ?", ids).Count(ctx, "*")
	case models.ResourceNotification:
		return gorm.G[models.Notification](tx).Where("id IN ?", ids).Count(ctx, "*")
	default:
		return 0, errInvalidPermission
	}
}

// uniqueIds removes empty and duplicate IDs, keeping the order.
func uniqueIds(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok || id == "" {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

func replaceRolePermissions(ctx context.Context, tx *gorm.DB, roleId string, permissions []models.RolePermission) error {
	if _, err := gorm.G[models.RolePermission](tx).Where("role_id = ?", roleId).Delete(ctx); err != nil {
		return err
	}
	if len(permissions) == 0 {
		return nil
	}
	for i := range permissions {
		permissions[i].RoleId = roleId
	}
	return gorm.G[models.RolePermission](tx).CreateInBatches(ctx, &permissions, 500)
}

func loadRole(ctx context.Context, id string) (models.Role, error) {
	role, err := gorm.G[models.Role](db.DB).Preload("Permissions", nil).Preload("OIDCGroups", nil).Where("id = ?", id).First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return role, errRoleNotFound
	}
	return role, err
}

func roleOutputOf(ctx context.Context, role *models.Role) (*roleOutput, error) {
	membersByRoleId, err := loadMembersByRoleId(ctx, []string{role.Id})
	if err != nil {
		return nil, internalError()
	}
	return &roleOutput{Body: toRoleResponse(role, membersByRoleId[role.Id])}, nil
}

func loadMembersByRoleId(ctx context.Context, roleIds []string) (map[string][]roleMemberResponse, error) {
	membersByRoleId := make(map[string][]roleMemberResponse, len(roleIds))
	if len(roleIds) == 0 {
		return membersByRoleId, nil
	}

	assignments, err := gorm.G[models.UserRole](db.DB).Where("role_id IN ?", roleIds).Find(ctx)
	if err != nil {
		return nil, err
	}

	userIds := make([]string, 0, len(assignments))
	for i := range assignments {
		userIds = append(userIds, assignments[i].UserId)
	}
	userIds = uniqueIds(userIds)
	users := map[string]models.User{}
	if len(userIds) > 0 {
		found, err := gorm.G[models.User](db.DB).Where("id IN ?", userIds).Find(ctx)
		if err != nil {
			return nil, err
		}
		for i := range found {
			users[found[i].Id] = found[i]
		}
	}

	// A user can have a role from several sources, which are merged into one member.
	type memberKey struct{ roleId, userId string }
	indexOf := make(map[memberKey]int, len(assignments))
	for i := range assignments {
		a := &assignments[i]
		user, ok := users[a.UserId]
		if !ok {
			continue
		}
		key := memberKey{a.RoleId, a.UserId}
		idx, ok := indexOf[key]
		if !ok {
			idx = len(membersByRoleId[a.RoleId])
			indexOf[key] = idx
			membersByRoleId[a.RoleId] = append(membersByRoleId[a.RoleId], roleMemberResponse{Id: user.Id, Name: user.Name, Email: user.Email})
		}
		members := membersByRoleId[a.RoleId]
		members[idx].Sources = append(members[idx].Sources, a.Source)
	}

	for roleId, members := range membersByRoleId {
		for i := range members {
			slices.Sort(members[i].Sources)
		}
		slices.SortFunc(members, func(a, b roleMemberResponse) int {
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		})
		membersByRoleId[roleId] = members
	}

	return membersByRoleId, nil
}

func toRoleResponse(role *models.Role, members []roleMemberResponse) roleResponse {
	permissions := make([]rolePermissionResponse, 0, len(role.Permissions))
	for i := range role.Permissions {
		p := &role.Permissions[i]
		permissions = append(permissions, rolePermissionResponse{Resource: p.Resource, Action: p.Action, ResourceId: p.ResourceId})
	}
	if members == nil {
		members = []roleMemberResponse{}
	}
	oidcGroups := make([]roleOIDCGroup, 0, len(role.OIDCGroups))
	for _, group := range role.OIDCGroups {
		oidcGroups = append(oidcGroups, roleOIDCGroup{ProviderId: group.ProviderId, Group: group.Group})
	}
	slices.SortFunc(oidcGroups, func(a, b roleOIDCGroup) int {
		return cmp.Or(strings.Compare(a.ProviderId, b.ProviderId), strings.Compare(a.Group, b.Group))
	})

	return roleResponse{
		Id:          role.Id,
		Name:        role.Name,
		Description: role.Description,
		BuiltIn:     role.BuiltIn,
		OIDCGroups:  oidcGroups,
		Permissions: permissions,
		Members:     members,
		CreatedAt:   role.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   role.UpdatedAt.Format(time.RFC3339),
	}
}

// compareRoles sorts the built-in roles first, then by name.
func compareRoles(a, b models.Role) int {
	return compareUserRoles(
		userRoleResponse{Name: a.Name, BuiltIn: a.BuiltIn},
		userRoleResponse{Name: b.Name, BuiltIn: b.BuiltIn},
	)
}
