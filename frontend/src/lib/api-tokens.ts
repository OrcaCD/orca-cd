import { fetcher } from "./api";
import type { Permissions } from "./permissions";
import { actionLabels, resourceLabels } from "./role-draft";
import { m } from "@/lib/paraglide/messages";
import {
	permissionActions,
	permissionResources,
	type PermissionAction,
	type PermissionResource,
} from "./roles";

// Either "admin" or a permission written as "resource:action"
export type ApiTokenScope = "admin" | `${PermissionResource}:${PermissionAction}`;

export interface ApiToken {
	id: string;
	name: string;
	// The start of the token, so tokens can be told apart
	prefix: string;
	scopes: ApiTokenScope[];
	expiresAt: string;
	lastUsedAt: string | null;
	createdAt: string;
	// Only set in the admin list
	owner?: ApiTokenOwner;
}

export interface ApiTokenOwner {
	id: string;
	name: string;
	email: string;
}

export interface CreatedApiToken extends ApiToken {
	// The plain token, which is only returned once
	token: string;
}

export interface CreateApiTokenRequest {
	name: string;
	scopes: ApiTokenScope[];
	expiresAt: string;
}

export const apiTokensPath = "/api-tokens";
export const adminApiTokensPath = "/admin/api-tokens";

// The token lists reload through SSE events, like the other resources.
export function createApiToken(data: CreateApiTokenRequest): Promise<CreatedApiToken> {
	return fetcher<CreatedApiToken>(apiTokensPath, "POST", data);
}

export async function revokeApiToken(id: string): Promise<void> {
	await fetcher(`${apiTokensPath}/${id}`, "DELETE");
}

// Revokes the token of any user
export async function adminRevokeApiToken(id: string): Promise<void> {
	await fetcher(`${adminApiTokensPath}/${id}`, "DELETE");
}

export function apiTokenScope(
	resource: PermissionResource,
	action: PermissionAction,
): ApiTokenScope {
	return `${resource}:${action}`;
}

export function apiTokenScopeLabel(scope: ApiTokenScope): string {
	const permission = parseApiTokenScope(scope);
	if (!permission) {
		return m.apiTokenScopeAdmin();
	}
	return `${resourceLabels[permission.resource]()}: ${actionLabels[permission.action]()}`;
}

export function parseApiTokenScope(
	scope: ApiTokenScope,
): { resource: PermissionResource; action: PermissionAction } | null {
	if (scope === "admin") {
		return null;
	}
	const [resource, action] = scope.split(":") as [PermissionResource, PermissionAction];
	return { resource, action };
}

// The permission scopes a user can give a token, grouped by resource. Like the
// hub, a grant on a single item is enough, as the token is then limited to it.
export function grantableApiTokenScopes(
	permissions: Permissions,
): { resource: PermissionResource; actions: PermissionAction[] }[] {
	return (
		permissionResources
			// A token can't create other tokens
			.filter((resource) => resource !== "api_token")
			.map((resource) => ({
				resource,
				actions: permissionActions[resource].filter((action) =>
					permissions.canAny(resource, action),
				),
			}))
			.filter(({ actions }) => actions.length > 0)
	);
}
