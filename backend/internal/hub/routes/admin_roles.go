package routes

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type rolePermissionRequest struct {
	Resource   models.PermissionResource `json:"resource" binding:"required"`
	Action     models.PermissionAction   `json:"action" binding:"required"`
	ResourceId *string                   `json:"resourceId"`
}

type upsertRoleRequest struct {
	Name        string                  `json:"name" binding:"required,min=1,max=64"`
	Description string                  `json:"description" binding:"max=256"`
	Permissions []rolePermissionRequest `json:"permissions" binding:"max=5000,dive"`
	// UserIds are the members an admin assigned by hand. Members from OIDC groups are not affected.
	UserIds []string `json:"userIds" binding:"max=10000"`
}

type rolePermissionResponse struct {
	Resource   models.PermissionResource `json:"resource"`
	Action     models.PermissionAction   `json:"action"`
	ResourceId *string                   `json:"resourceId"`
}

type roleMemberResponse struct {
	Id      string                  `json:"id"`
	Name    string                  `json:"name"`
	Email   string                  `json:"email"`
	Sources []models.UserRoleSource `json:"sources"`
}

type roleResponse struct {
	Id          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	BuiltIn     *models.RoleBuiltIn      `json:"builtIn"`
	Permissions []rolePermissionResponse `json:"permissions"`
	Members     []roleMemberResponse     `json:"members"`
	CreatedAt   string                   `json:"createdAt"`
	UpdatedAt   string                   `json:"updatedAt"`
}

var errRoleNotFound = errors.New("role not found")

func AdminListRolesHandler(c *gin.Context) {
	ctx := c.Request.Context()

	roles, err := gorm.G[models.Role](db.DB).Preload("Permissions", nil).Find(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	slices.SortFunc(roles, compareRoles)

	roleIds := make([]string, 0, len(roles))
	for i := range roles {
		roleIds = append(roleIds, roles[i].Id)
	}
	membersByRoleId, err := loadMembersByRoleId(ctx, roleIds)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	response := make([]roleResponse, 0, len(roles))
	for i := range roles {
		response = append(response, toRoleResponse(&roles[i], membersByRoleId[roles[i].Id]))
	}

	c.JSON(http.StatusOK, response)
}

func AdminGetRoleHandler(c *gin.Context) {
	role, err := loadRole(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeRoleError(c, err)
		return
	}

	writeRole(c, http.StatusOK, &role)
}

func AdminCreateRoleHandler(c *gin.Context) {
	req, permissions, ok := bindRoleRequest(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	role := models.Role{
		Name:        req.Name,
		Description: req.Description,
	}

	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkRoleReferences(ctx, tx, permissions, req.UserIds); err != nil {
			return err
		}
		if err := gorm.G[models.Role](tx).Create(ctx, &role); err != nil {
			return err
		}
		if err := replaceRolePermissions(ctx, tx, role.Id, permissions); err != nil {
			return err
		}
		_, err := rbac.SetRoleMembers(ctx, tx, role.Id, models.UserRoleSourceManual, req.UserIds)
		return err
	})
	if err != nil {
		writeRoleError(c, err)
		return
	}

	role, err = loadRole(ctx, role.Id)
	if err != nil {
		writeRoleError(c, err)
		return
	}

	utils.RecordAuditLog(c, "created", "role", role.Id)

	writeRole(c, http.StatusCreated, &role)
	sse.PublishUpdate(sse.TopicAdminRoles, sse.TopicAdminUsers, sse.TopicProfile)
}

func AdminUpdateRoleHandler(c *gin.Context) {
	req, permissions, ok := bindRoleRequest(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	id := c.Param("id")
	membersChanged := false

	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		role, err := gorm.G[models.Role](tx).Where("id = ?", id).First(ctx)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errRoleNotFound
			}
			return err
		}
		if err := checkRoleReferences(ctx, tx, permissions, req.UserIds); err != nil {
			return err
		}

		// The names of the built-in roles are translated by clients, and the
		// permissions of the admin role are implicit.
		if role.BuiltIn == nil {
			if _, err := gorm.G[models.Role](tx).Where("id = ?", id).
				Select("name", "description", "updated_at").
				Updates(ctx, models.Role{Name: req.Name, Description: req.Description, UpdatedAt: time.Now()}); err != nil {
				return err
			}
		}
		if !role.IsBuiltIn(models.RoleBuiltInAdmin) {
			if err := replaceRolePermissions(ctx, tx, id, permissions); err != nil {
				return err
			}
		}

		return keepLastAdmin(ctx, tx, func() error {
			var err error
			membersChanged, err = rbac.SetRoleMembers(ctx, tx, id, models.UserRoleSourceManual, req.UserIds)
			return err
		})
	})
	if err != nil {
		if errors.Is(err, errLastAdmin) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot remove the last admin user"})
			return
		}
		writeRoleError(c, err)
		return
	}

	role, err := loadRole(ctx, id)
	if err != nil {
		writeRoleError(c, err)
		return
	}

	utils.RecordAuditLog(c, "updated", "role", id)
	if membersChanged {
		utils.RecordAuditLog(c, "members-updated", "role", id)
	}

	writeRole(c, http.StatusOK, &role)
	sse.PublishUpdate(sse.TopicAdminRoles, sse.TopicAdminUsers, sse.TopicProfile)
}

func AdminDeleteRoleHandler(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

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
		writeRoleError(c, err)
		return
	}

	utils.RecordAuditLog(c, "deleted", "role", id)

	c.JSON(http.StatusOK, gin.H{"message": "role deleted"})
	sse.PublishUpdate(sse.TopicAdminRoles, sse.TopicAdminUsers, sse.TopicProfile)
}

var (
	errBuiltInRole       = errors.New("built-in roles cannot be deleted")
	errInvalidPermission = errors.New("invalid permission")
	errUnknownResource   = errors.New("permission refers to a resource that does not exist")
	errUnknownUser       = errors.New("user not found")
)

func writeRoleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errRoleNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "role not found"})
	case errors.Is(err, errBuiltInRole), errors.Is(err, errUnknownResource), errors.Is(err, errUnknownUser):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case isUniqueConstraintError(err):
		c.JSON(http.StatusConflict, gin.H{"error": "a role with this name already exists"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

// bindRoleRequest parses and validates a role, returning its deduplicated
// permissions. Whether the referenced users and resources exist is checked by
// checkRoleReferences in the write transaction.
func bindRoleRequest(c *gin.Context) (upsertRoleRequest, []models.RolePermission, bool) {
	var req upsertRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name is required and must be at most 64 characters"})
		return req, nil, false
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name is required and must be at most 64 characters"})
		return req, nil, false
	}

	permissions, err := normalizeRolePermissions(req.Permissions)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return req, nil, false
	}
	req.UserIds = uniqueIds(req.UserIds)

	return req, permissions, true
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

// checkRoleReferences checks that the users and resources a role refers to exist.
func checkRoleReferences(ctx context.Context, tx *gorm.DB, permissions []models.RolePermission, userIds []string) error {
	idsByResource := make(map[models.PermissionResource][]string)
	for i := range permissions {
		if id := permissions[i].ResourceId; id != nil {
			idsByResource[permissions[i].Resource] = append(idsByResource[permissions[i].Resource], *id)
		}
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
	role, err := gorm.G[models.Role](db.DB).Preload("Permissions", nil).Where("id = ?", id).First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return role, errRoleNotFound
	}
	return role, err
}

func writeRole(c *gin.Context, status int, role *models.Role) {
	membersByRoleId, err := loadMembersByRoleId(c.Request.Context(), []string{role.Id})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(status, toRoleResponse(role, membersByRoleId[role.Id]))
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

	return roleResponse{
		Id:          role.Id,
		Name:        role.Name,
		Description: role.Description,
		BuiltIn:     role.BuiltIn,
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
