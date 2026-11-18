package models

import (
	"slices"
	"strings"
	"time"
)

// APITokenScope is something an API token is allowed to do. It is either a
// permission written as "resource:action" or APITokenScopeAdmin.
type APITokenScope string

// APITokenScopeAdmin lets a token use the admin routes. It has no effect unless
// the owner of the token is an admin.
const APITokenScopeAdmin APITokenScope = "admin"

func NewAPITokenScope(resource PermissionResource, action PermissionAction) APITokenScope {
	return APITokenScope(string(resource) + ":" + string(action))
}

// Permission returns the resource and action of a permission scope. It reports
// false for the admin scope and for scopes that are not "resource:action".
func (s APITokenScope) Permission() (PermissionResource, PermissionAction, bool) {
	resource, action, ok := strings.Cut(string(s), ":")
	if !ok || resource == "" || action == "" {
		return "", "", false
	}
	return PermissionResource(resource), PermissionAction(action), true
}

// AllAPITokenScopes lists every scope a token can be given, in display order:
// the admin scope, then the permissions in the order of PermissionResources and
// PermissionActions. A token gets only the scopes it was created with.
func AllAPITokenScopes() []APITokenScope {
	scopes := []APITokenScope{APITokenScopeAdmin}
	for _, resource := range PermissionResources {
		if resource == ResourceAPIToken {
			continue
		}
		for _, action := range PermissionActions[resource] {
			scopes = append(scopes, NewAPITokenScope(resource, action))
		}
	}
	return scopes
}

func IsValidAPITokenScope(scope APITokenScope) bool {
	if scope == APITokenScopeAdmin {
		return true
	}
	resource, action, ok := scope.Permission()
	// A token cannot create other tokens.
	return ok && resource != ResourceAPIToken && IsValidPermission(resource, action)
}

// APIToken is a personal access token a user calls the REST API with. A token
// can never do more than its owner; its scopes only narrow the owner's access.
type APIToken struct {
	Base
	UserId string `gorm:"type:text;not null"`
	User   *User  `gorm:"foreignKey:UserId"`
	Name   string `gorm:"type:text;not null"`
	// TokenHash is the SHA-512 hash of the plain token, which is never stored.
	TokenHash string `gorm:"type:text;not null;uniqueIndex"`
	// Prefix is the start of the plain token, so users can tell their tokens apart.
	Prefix     string          `gorm:"type:text;not null"`
	Scopes     []APITokenScope `gorm:"type:text;not null;default:'[]';serializer:json"`
	ExpiresAt  time.Time       `gorm:"not null"`
	LastUsedAt *time.Time
}

func (APIToken) TableName() string {
	return "api_tokens"
}

func (t *APIToken) IsExpired(now time.Time) bool {
	return !now.Before(t.ExpiresAt)
}

func (t *APIToken) HasScope(scope APITokenScope) bool {
	return slices.Contains(t.Scopes, scope)
}
