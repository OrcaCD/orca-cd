import { fetcher } from "./api";
import { m } from "@/lib/paraglide/messages";

export type PermissionResource = "application" | "repository" | "agent" | "notification";

export type PermissionAction =
	| "read"
	| "create"
	| "update"
	| "delete"
	| "deploy"
	| "pull_images"
	| "rollback"
	| "sync";

export type RoleBuiltIn = "admin" | "user";

export type UserRoleSource = "manual" | "oidc";

export const permissionResources: PermissionResource[] = [
	"application",
	"repository",
	"agent",
	"notification",
];

// Mirrors models.PermissionActions in the hub
export const permissionActions: Record<PermissionResource, PermissionAction[]> = {
	application: ["read", "create", "update", "delete", "deploy", "pull_images", "rollback"],
	repository: ["read", "create", "update", "delete", "sync"],
	agent: ["read", "create", "update", "delete"],
	notification: ["read", "create", "update", "delete"],
};

export interface RolePermission {
	resource: PermissionResource;
	action: PermissionAction;
	resourceId: string | null;
}

export interface RoleMember {
	id: string;
	name: string;
	email: string;
	sources: UserRoleSource[];
}

export interface Role {
	id: string;
	name: string;
	description: string;
	builtIn: RoleBuiltIn | null;
	// Users in any of these groups at their OIDC provider get the role
	oidcGroups: RoleOIDCGroup[];
	permissions: RolePermission[];
	members: RoleMember[];
	createdAt: string;
	updatedAt: string;
}

// A role as assigned to a user
export interface UserRole {
	id: string;
	name: string;
	builtIn: RoleBuiltIn | null;
	sources: UserRoleSource[];
}

export interface UpsertRoleRequest {
	name: string;
	description: string;
	permissions: RolePermission[];
	userIds: string[];
	oidcGroups: RoleOIDCGroup[];
}

// Group names are only unique within their provider
export interface RoleOIDCGroup {
	providerId: string;
	group: string;
}

// The names of the built-in roles are translated instead of using the stored ones.
export function roleName(role: { name: string; builtIn: RoleBuiltIn | null }): string {
	switch (role.builtIn) {
		case "admin":
			return m.roleAdmin();
		case "user":
			return m.roleUser();
		default:
			return role.name;
	}
}

export function roleDescription(role: {
	description: string;
	builtIn: RoleBuiltIn | null;
}): string {
	switch (role.builtIn) {
		case "admin":
			return m.roleAdminDescription();
		case "user":
			return m.roleUserDescription();
		default:
			return role.description;
	}
}

export function createRole(data: UpsertRoleRequest): Promise<Role> {
	return fetcher<Role>("/admin/roles", "POST", data);
}

export function updateRole(id: string, data: UpsertRoleRequest): Promise<Role> {
	return fetcher<Role>(`/admin/roles/${id}`, "PUT", data);
}

export async function deleteRole(id: string): Promise<void> {
	await fetcher(`/admin/roles/${id}`, "DELETE");
}
