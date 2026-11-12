import { useCallback, useMemo } from "react";
import { useFetch } from "./api";
import type { ApplicationListItem } from "./applications";
import { useAuth, type Profile } from "./auth";
import type { PermissionAction, PermissionResource } from "./roles";

export interface Permissions {
	// Whether the action is allowed on the resource. Without an ID, it has to be
	// allowed on all resources of the type, like for creating one.
	can: (resource: PermissionResource, action: PermissionAction, id?: string) => boolean;
	// Whether the action is allowed on at least one resource of the type
	canAny: (resource: PermissionResource, action: PermissionAction) => boolean;
}

// Mirrors rbac.Permissions in the hub, which enforces them. The UI only uses them
// to hide what the user is not allowed to do.
export function permissionsOf(profile: Profile | null): Permissions {
	const grantOf = (resource: PermissionResource) => profile?.permissions?.[resource];

	const can: Permissions["can"] = (resource, action, id) => {
		if (profile?.isAdmin) {
			return true;
		}
		const grant = grantOf(resource);
		if (!grant) {
			return false;
		}
		if (grant.all.includes(action)) {
			return true;
		}
		return id !== undefined && (grant.resources[id]?.includes(action) ?? false);
	};

	const canAny: Permissions["canAny"] = (resource, action) => {
		if (can(resource, action)) {
			return true;
		}
		return Object.values(grantOf(resource)?.resources ?? {}).some((actions) =>
			actions.includes(action),
		);
	};

	return { can, canAny };
}

export function usePermissions(): Permissions {
	const { auth } = useAuth();
	return useMemo(() => permissionsOf(auth.profile), [auth.profile]);
}

// Returns whether an action is allowed on all applications of a repository or
// agent, which the hub checks when syncing or deleting it. Grants on single
// applications only count when the user can see all of them.
export function useCanOnApplicationsOf(by: "repositoryId" | "agentId") {
	const { can } = usePermissions();
	const { data: applications } = useFetch<ApplicationListItem[]>("/applications");
	return useCallback(
		(id: string, action: PermissionAction, appCount: number | undefined) => {
			if (appCount === 0 || can("application", action)) {
				return true;
			}
			const visible = applications?.filter((app) => app[by] === id) ?? [];
			return (
				appCount !== undefined &&
				visible.length === appCount &&
				visible.every((app) => can("application", action, app.id))
			);
		},
		[applications, by, can],
	);
}
