import { m } from "@/lib/paraglide/messages";
import type { RepositorySyncType } from "@/lib/repositories";
import { MousePointerClickIcon, RefreshCw, WebhookIcon } from "lucide-react";

function getSyncTypeIcon(syncType: RepositorySyncType) {
	switch (syncType) {
		case "webhook":
			return <WebhookIcon className="h-4 w-4" />;
		case "polling":
			return <RefreshCw className="h-4 w-4" />;
		case "manual":
			return <MousePointerClickIcon className="h-4 w-4" />;
		default:
			return null;
	}
}

function getSyncTypeLabel(syncType: RepositorySyncType): string {
	switch (syncType) {
		case "webhook":
			return m.repoSyncTypeWebhook();
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
