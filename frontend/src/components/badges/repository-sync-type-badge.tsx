import { m } from "@/lib/paraglide/messages";
import type { RepositorySyncType } from "@/lib/repositories";
import { MousePointerClickIcon, RefreshCw, WebhookIcon } from "lucide-react";

export function getSyncTypeIcon(syncType: RepositorySyncType, className = "h-4 w-4") {
	switch (syncType) {
		case "webhook":
			return <WebhookIcon className={className} />;
		case "polling":
			return <RefreshCw className={className} />;
		case "manual":
			return <MousePointerClickIcon className={className} />;
		default:
			return null;
	}
}

export function getSyncTypeLabel(syncType: RepositorySyncType): string {
	switch (syncType) {
		case "webhook":
			return m.webhook();
		case "polling":
			return m.repoSyncTypePolling();
		case "manual":
			return m.repoSyncTypeManual();
		default:
			return m.unknown();
	}
}

export function RepositorySyncTypeBadge({ syncType }: { syncType: RepositorySyncType }) {
	return (
		<div className="flex items-center gap-2">
			{getSyncTypeIcon(syncType)}
			{getSyncTypeLabel(syncType)}
		</div>
	);
}
