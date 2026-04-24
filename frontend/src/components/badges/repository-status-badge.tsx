import { Badge } from "../ui/badge";
import { m } from "@/lib/paraglide/messages";
import type { RepositorySyncStatus } from "@/lib/repsitories";

function getSyncStatusColor(syncStatus: RepositorySyncStatus): string {
	switch (syncStatus) {
		case "syncing":
			return "bg-blue-500";
		case "failed":
			return "bg-red-500";
		case "success":
			return "bg-green-500";
		default:
			return "bg-gray-500";
	}
}

function getSyncStatusLabel(syncStatus: RepositorySyncStatus): string {
	switch (syncStatus) {
		case "syncing":
			return m.repoSyncStatusSyncing();
		case "failed":
			return m.repoSyncStatusFailed();
		case "success":
			return m.repoSyncStatusSuccess();
		default:
			return m.unknown();
	}
}

export function RepositoryStatusBadge({ status }: { status: RepositorySyncStatus }) {
	const statusLabel = getSyncStatusLabel(status);
	const statusDotClass = getSyncStatusColor(status);
	const badgeVariant =
		status === "success" ? "success" : status === "failed" ? "destructive" : "secondary";

	return (
		<div className="flex items-center gap-2">
			<Badge variant={badgeVariant}>
				<span className={`h-2 w-2 rounded-full ${statusDotClass}`} aria-hidden="true" />
				{statusLabel}
			</Badge>
		</div>
	);
}
