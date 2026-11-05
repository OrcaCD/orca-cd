package models

import (
	"slices"
	"time"
)

// PermissionResource is a resource type a role can grant access to.
type PermissionResource string

const (
	ResourceApplication  PermissionResource = "application"
	ResourceRepository   PermissionResource = "repository"
	ResourceAgent        PermissionResource = "agent"
	ResourceNotification PermissionResource = "notification"
)

// PermissionAction is something a role allows to do with a resource.
type PermissionAction string

const (
	ActionRead       PermissionAction = "read"
	ActionCreate     PermissionAction = "create"
	ActionUpdate     PermissionAction = "update"
	ActionDelete     PermissionAction = "delete"
	ActionDeploy     PermissionAction = "deploy"
	ActionPullImages PermissionAction = "pull_images"
	ActionRollback   PermissionAction = "rollback"
	ActionSync       PermissionAction = "sync"
)

// PermissionResources lists every resource type in display order.
var PermissionResources = []PermissionResource{
	ResourceApplication,
	ResourceRepository,
	ResourceAgent,
	ResourceNotification,
}

// PermissionActions lists the actions each resource type supports, in display order.
var PermissionActions = map[PermissionResource][]PermissionAction{
	ResourceApplication:  {ActionRead, ActionCreate, ActionUpdate, ActionDelete, ActionDeploy, ActionPullImages, ActionRollback},
	ResourceRepository:   {ActionRead, ActionCreate, ActionUpdate, ActionDelete, ActionSync},
	ResourceAgent:        {ActionRead, ActionCreate, ActionUpdate, ActionDelete},
	ResourceNotification: {ActionRead, ActionCreate, ActionUpdate, ActionDelete},
}

// IsValidPermission reports whether the action exists for the resource type.
func IsValidPermission(resource PermissionResource, action PermissionAction) bool {
	return slices.Contains(PermissionActions[resource], action)
}

// RoleBuiltIn identifies the roles every installation has. They cannot be deleted.
type RoleBuiltIn string

const (
	// RoleBuiltInAdmin can do everything, including the admin pages. Its
	// permissions are implicit and cannot be changed.
	RoleBuiltInAdmin RoleBuiltIn = "admin"
	// RoleBuiltInUser is given to new users. It starts with full access to all
	// resources, which matches the behavior before roles existed.
	RoleBuiltInUser RoleBuiltIn = "user"
)

type Role struct {
	Base
	BuiltIn     *RoleBuiltIn `gorm:"column:built_in;type:text"`
	Name        string       `gorm:"type:text;not null"`
	Description string       `gorm:"type:text;not null;default:''"`
	// OIDCGroups gives the role to users who are in any of these groups at their OIDC provider.
	OIDCGroups  []string         `gorm:"column:oidc_groups;type:text;not null;default:'[]';serializer:json"`
	Permissions []RolePermission `gorm:"foreignKey:RoleId"`
}

func (r *Role) IsBuiltIn(builtIn RoleBuiltIn) bool {
	return r.BuiltIn != nil && *r.BuiltIn == builtIn
}

func (Role) TableName() string {
	return "roles"
}

// RolePermission allows an action on all resources of a type, or on a single
// resource when ResourceId is set.
type RolePermission struct {
	Base
	RoleId     string             `gorm:"type:text;not null;index"`
	Resource   PermissionResource `gorm:"type:text;not null"`
	Action     PermissionAction   `gorm:"type:text;not null"`
	ResourceId *string            `gorm:"type:text"`
}

func (RolePermission) TableName() string {
	return "role_permissions"
}

// UserRoleSource tells how a user got a role. Roles from OIDC groups are synced on
// login and never touch the roles an admin assigned by hand.
type UserRoleSource string

const (
	UserRoleSourceManual UserRoleSource = "manual"
	UserRoleSourceOIDC   UserRoleSource = "oidc"
)

type UserRole struct {
	UserId    string         `gorm:"type:text;primaryKey"`
	RoleId    string         `gorm:"type:text;primaryKey;index"`
	Source    UserRoleSource `gorm:"type:text;primaryKey;default:'manual'"`
	CreatedAt time.Time
	Role      *Role `gorm:"foreignKey:RoleId"`
}

func (UserRole) TableName() string {
	return "user_roles"
}
