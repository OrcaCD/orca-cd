import { Bell, Container, GitBranch, Server, type LucideIcon } from "lucide-react";
import { m } from "@/lib/paraglide/messages";
import {
	permissionActions,
	permissionResources,
	type PermissionAction,
	type PermissionResource,
	type Role,
	type RoleOIDCGroup,
	type RolePermission,
	type UpsertRoleRequest,
} from "./roles";

// Grants of a role, keyed by resource type and then by resource ID. ALL holds the
// actions allowed on every resource of the type.
export const ALL = "*";
export type Grants = Record<PermissionResource, Record<string, PermissionAction[]>>;

export interface RoleDraft {
	name: string;
	description: string;
	grants: Grants;
	userIds: string[];
	oidcGroups: RoleOIDCGroup[];
}

export function emptyGrants(): Grants {
	return { application: {}, repository: {}, agent: {}, notification: {} };
}

export function toDraft(role: Role | null): RoleDraft {
	const grants = emptyGrants();
	for (const permission of role?.permissions ?? []) {
		const key = permission.resourceId ?? ALL;
		const actions = grants[permission.resource][key] ?? [];
		grants[permission.resource][key] = [...actions, permission.action];
	}
	for (const resource of permissionResources) {
		for (const key of Object.keys(grants[resource])) {
			grants[resource][key] = sortActions(resource, grants[resource][key]);
		}
	}

	return {
		name: role?.name ?? "",
		description: role?.description ?? "",
		grants,
		userIds:
			role?.members
				.filter((member) => member.sources.includes("manual"))
				.map((member) => member.id) ?? [],
		oidcGroups: role?.oidcGroups ?? [],
	};
}

export function toRequest(draft: RoleDraft): UpsertRoleRequest {
	const permissions: RolePermission[] = [];
	for (const resource of permissionResources) {
		for (const [key, actions] of Object.entries(draft.grants[resource])) {
			for (const action of actions) {
				permissions.push({ resource, action, resourceId: key === ALL ? null : key });
			}
		}
	}
	return {
		name: draft.name.trim(),
		description: draft.description.trim(),
		permissions,
		userIds: draft.userIds,
		oidcGroups: draft.oidcGroups,
	};
}

// Compares drafts independently of the order grants and members were added in.
export function draftKey(draft: RoleDraft): string {
	const request = toRequest(draft);
	return JSON.stringify({
		...request,
		permissions: request.permissions
			.map((p) => `${p.resource}:${p.resourceId ?? ALL}:${p.action}`)
			.sort(),
		userIds: [...request.userIds].sort(),
		oidcGroups: request.oidcGroups.map((g) => `${g.providerId}:${g.group}`).sort(),
	});
}

export function sortActions(
	resource: PermissionResource,
	actions: PermissionAction[],
): PermissionAction[] {
	return permissionActions[resource].filter((action) => actions.includes(action));
}

// Every action includes viewing the resource, as the hub grants read with it.
export function isReadImplied(actions: PermissionAction[]): boolean {
	return actions.some((action) => action !== "read");
}

// Actions that can be granted on a single resource. Creating is not tied to one.
export function scopedActions(resource: PermissionResource): PermissionAction[] {
	return permissionActions[resource].filter((action) => action !== "create");
}

export function setActions(
	grants: Grants,
	resource: PermissionResource,
	key: string,
	actions: PermissionAction[],
): Grants {
	const next = { ...grants, [resource]: { ...grants[resource] } };
	if (actions.length === 0 && key === ALL) {
		delete next[resource][key];
	} else {
		next[resource][key] = sortActions(resource, actions);
	}
	if (key === ALL) {
		// Actions allowed on all resources don't need to be granted on single ones
		const redundant = (action: PermissionAction) =>
			actions.includes(action) || (action === "read" && isReadImplied(actions));
		for (const [id, granted] of Object.entries(next[resource])) {
			if (id === ALL) {
				continue;
			}
			const remaining = granted.filter((action) => !redundant(action));
			// Rows left with nothing to grant go away, empty rows the user added stay
			if (remaining.length === 0 && granted.length > 0) {
				delete next[resource][id];
			} else {
				next[resource][id] = remaining;
			}
		}
	}
	return next;
}

export function removeResource(grants: Grants, resource: PermissionResource, key: string): Grants {
	const next = { ...grants, [resource]: { ...grants[resource] } };
	delete next[resource][key];
	return next;
}

// Whether the grants allow reading at least one resource of the type
export function canReadAny(grants: Grants, resource: PermissionResource): boolean {
	return Object.values(grants[resource]).some((actions) => actions.length > 0);
}

export function countGrants(grants: Grants): number {
	return permissionResources.reduce(
		(sum, resource) =>
			sum + Object.values(grants[resource]).reduce((count, actions) => count + actions.length, 0),
		0,
	);
}

export const resourceIcons: Record<PermissionResource, LucideIcon> = {
	application: Container,
	repository: GitBranch,
	agent: Server,
	notification: Bell,
};

export const resourceLabels: Record<PermissionResource, () => string> = {
	application: () => m.navApplications(),
	repository: () => m.navRepositories(),
	agent: () => m.navAgents(),
	notification: () => m.navNotifications(),
};

export const allResourcesLabels: Record<PermissionResource, () => string> = {
	application: () => m.allApplications(),
	repository: () => m.allRepositories(),
	agent: () => m.allAgents(),
	notification: () => m.allNotifications(),
};

export const actionLabels: Record<PermissionAction, () => string> = {
	read: () => m.permissionRead(),
	create: () => m.permissionCreate(),
	update: () => m.permissionUpdate(),
	delete: () => m.permissionDelete(),
	deploy: () => m.permissionDeploy(),
	pull_images: () => m.permissionPullImages(),
	rollback: () => m.permissionRollback(),
	sync: () => m.permissionSync(),
};

export function actionDescription(resource: PermissionResource, action: PermissionAction): string {
	switch (`${resource}.${action}`) {
		case "application.update":
			return m.permissionApplicationUpdateDescription();
		case "application.deploy":
			return m.permissionApplicationDeployDescription();
		case "application.pull_images":
			return m.permissionApplicationPullImagesDescription();
		case "application.rollback":
			return m.permissionApplicationRollbackDescription();
		case "application.delete":
			return m.permissionApplicationDeleteDescription();
		case "repository.update":
			return m.permissionRepositoryUpdateDescription();
		case "repository.sync":
			return m.permissionRepositorySyncDescription();
		case "repository.delete":
			return m.permissionRepositoryDeleteDescription();
		case "agent.update":
			return m.permissionAgentUpdateDescription();
		case "agent.delete":
			return m.permissionAgentDeleteDescription();
		case "notification.update":
			return m.permissionNotificationUpdateDescription();
		default:
			return "";
	}
}
