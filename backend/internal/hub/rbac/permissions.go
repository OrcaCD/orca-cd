// Package rbac resolves what a user is allowed to do from the roles assigned to them.
package rbac

import (
	"context"
	"errors"
	"slices"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const permissionsContextKey = "rbac_permissions"

var ErrMissingUser = errors.New("rbac: no authenticated user in context")

type actionSet map[models.PermissionAction]struct{}

// Permissions is the union of all grants of a user's roles.
type Permissions struct {
	admin  bool
	global map[models.PermissionResource]actionSet
	scoped map[models.PermissionResource]map[string]actionSet
	roles  []AssignedRole
}

// Admin returns permissions that allow everything.
func Admin() *Permissions {
	return &Permissions{admin: true}
}

// New builds the permissions for a set of grants. Every action implies read
// access on the same resources, since nothing can be changed without seeing it.
func New(grants []models.RolePermission) *Permissions {
	p := &Permissions{
		global: make(map[models.PermissionResource]actionSet),
		scoped: make(map[models.PermissionResource]map[string]actionSet),
	}
	for i := range grants {
		g := &grants[i]
		if !models.IsValidPermission(g.Resource, g.Action) {
			continue
		}
		if g.ResourceId == nil {
			p.add(p.globalSet(g.Resource), g.Action)
			continue
		}
		// Creating is not tied to an existing resource.
		if g.Action == models.ActionCreate {
			continue
		}
		p.add(p.scopedSet(g.Resource, *g.ResourceId), g.Action)
	}
	return p
}

func (p *Permissions) add(set actionSet, action models.PermissionAction) {
	set[action] = struct{}{}
	set[models.ActionRead] = struct{}{}
}

func (p *Permissions) globalSet(resource models.PermissionResource) actionSet {
	set, ok := p.global[resource]
	if !ok {
		set = make(actionSet)
		p.global[resource] = set
	}
	return set
}

func (p *Permissions) scopedSet(resource models.PermissionResource, id string) actionSet {
	byID, ok := p.scoped[resource]
	if !ok {
		byID = make(map[string]actionSet)
		p.scoped[resource] = byID
	}
	set, ok := byID[id]
	if !ok {
		set = make(actionSet)
		byID[id] = set
	}
	return set
}

func (p *Permissions) IsAdmin() bool {
	return p != nil && p.admin
}

// CanAll reports whether the action is allowed on every resource of the type.
func (p *Permissions) CanAll(resource models.PermissionResource, action models.PermissionAction) bool {
	if p == nil {
		return false
	}
	if p.admin {
		return true
	}
	_, ok := p.global[resource][action]
	return ok
}

// Can reports whether the action is allowed on the resource with the given ID.
// An empty ID only matches grants on all resources of the type.
func (p *Permissions) Can(resource models.PermissionResource, action models.PermissionAction, id string) bool {
	if p.CanAll(resource, action) {
		return true
	}
	if p == nil || id == "" {
		return false
	}
	_, ok := p.scoped[resource][id][action]
	return ok
}

// CanAny reports whether the action is allowed on at least one resource of the type.
func (p *Permissions) CanAny(resource models.PermissionResource, action models.PermissionAction) bool {
	if p.CanAll(resource, action) {
		return true
	}
	if p == nil {
		return false
	}
	for _, set := range p.scoped[resource] {
		if _, ok := set[action]; ok {
			return true
		}
	}
	return false
}

// IDs returns the resources the action is allowed on. all is true when it is
// allowed on every resource of the type, ids is then nil.
func (p *Permissions) IDs(resource models.PermissionResource, action models.PermissionAction) (ids []string, all bool) {
	if p.CanAll(resource, action) {
		return nil, true
	}
	ids = []string{}
	if p == nil {
		return ids, false
	}
	for id, set := range p.scoped[resource] {
		if _, ok := set[action]; ok {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, false
}

// ResourceGrant lists the actions allowed on all resources of a type and on single resources.
type ResourceGrant struct {
	All       []models.PermissionAction            `json:"all"`
	Resources map[string][]models.PermissionAction `json:"resources"`
}

// Grants returns the effective permissions per resource type, so clients can
// hide what the user is not allowed to do.
func (p *Permissions) Grants() map[models.PermissionResource]ResourceGrant {
	grants := make(map[models.PermissionResource]ResourceGrant, len(models.PermissionResources))
	for _, resource := range models.PermissionResources {
		grant := ResourceGrant{
			All:       []models.PermissionAction{},
			Resources: map[string][]models.PermissionAction{},
		}
		for _, action := range models.PermissionActions[resource] {
			if p.CanAll(resource, action) {
				grant.All = append(grant.All, action)
			}
		}
		if p != nil && !p.admin {
			for id, set := range p.scoped[resource] {
				actions := []models.PermissionAction{}
				for _, action := range models.PermissionActions[resource] {
					if _, ok := set[action]; ok && !slices.Contains(grant.All, action) {
						actions = append(actions, action)
					}
				}
				if len(actions) > 0 {
					grant.Resources[id] = actions
				}
			}
		}
		grants[resource] = grant
	}
	return grants
}

// AssignedRole is a role of a user. Sources tells whether an admin assigned it,
// it comes from an OIDC group, or both.
type AssignedRole struct {
	Id      string
	Name    string
	BuiltIn *models.RoleBuiltIn
	Sources []models.UserRoleSource
}

// Roles returns the roles the permissions were loaded from.
func (p *Permissions) Roles() []AssignedRole {
	if p == nil {
		return nil
	}
	return p.roles
}

type grantRow struct {
	RoleId     string
	RoleName   string
	BuiltIn    *models.RoleBuiltIn
	Source     models.UserRoleSource
	Resource   *models.PermissionResource
	Action     *models.PermissionAction
	ResourceId *string
}

// Load resolves the permissions of a user from their roles. It runs on every
// authenticated request, so roles and grants are loaded with a single query.
func Load(ctx context.Context, tx *gorm.DB, userID string) (*Permissions, error) {
	rows, err := gorm.G[grantRow](tx).Raw(`
		SELECT roles.id AS role_id, roles.name AS role_name, roles.built_in, user_roles.source,
			role_permissions.resource, role_permissions.action, role_permissions.resource_id
		FROM user_roles
		JOIN roles ON roles.id = user_roles.role_id
		LEFT JOIN role_permissions ON role_permissions.role_id = roles.id
		WHERE user_roles.user_id = ?`, userID).Find(ctx)
	if err != nil {
		return nil, err
	}

	var (
		roles   []AssignedRole
		indexOf = make(map[string]int)
		grants  = make([]models.RolePermission, 0, len(rows))
		admin   bool
	)
	for i := range rows {
		row := &rows[i]
		idx, ok := indexOf[row.RoleId]
		if !ok {
			idx = len(roles)
			indexOf[row.RoleId] = idx
			roles = append(roles, AssignedRole{Id: row.RoleId, Name: row.RoleName, BuiltIn: row.BuiltIn})
		}
		// A role has at most one assignment per source
		if !slices.Contains(roles[idx].Sources, row.Source) {
			roles[idx].Sources = append(roles[idx].Sources, row.Source)
		}
		if row.BuiltIn != nil && *row.BuiltIn == models.RoleBuiltInAdmin {
			admin = true
		}
		if row.Resource != nil && row.Action != nil {
			grants = append(grants, models.RolePermission{Resource: *row.Resource, Action: *row.Action, ResourceId: row.ResourceId})
		}
	}
	for i := range roles {
		slices.Sort(roles[i].Sources)
	}

	p := Admin()
	if !admin {
		p = New(grants)
	}
	p.roles = roles
	return p, nil
}

// Set stores the permissions for the current request.
func Set(c *gin.Context, p *Permissions) {
	c.Set(permissionsContextKey, p)
}

// For returns the permissions of the authenticated user, loading them once per request.
func For(c *gin.Context) (*Permissions, error) {
	if val, ok := c.Get(permissionsContextKey); ok {
		if p, ok := val.(*Permissions); ok {
			return p, nil
		}
	}

	user, ok := auth.GetUser(c)
	if !ok {
		return nil, ErrMissingUser
	}

	p, err := Load(c.Request.Context(), db.DB, user.Id)
	if err != nil {
		return nil, err
	}
	Set(c, p)
	return p, nil
}
