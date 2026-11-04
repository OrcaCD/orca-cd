import type { HealthStatus, SyncStatus } from "@/lib/applications";
import { cn } from "@/lib/utils";
import { Badge } from "../ui/badge";
import { m } from "@/lib/paraglide/messages";

type StatusBadgeProps =
	| { type: "sync"; status: SyncStatus }
	| { type: "health"; status: HealthStatus };

const syncConfig: Record<SyncStatus, { label: () => string; className: string; dotClass: string }> =
	{
		synced: {
			label: () => m.statusSynced(),
			className: "bg-emerald-500/20 text-emerald-700 dark:text-emerald-400 border-emerald-500/30",
			dotClass: "bg-emerald-500 dark:bg-emerald-400",
		},
		out_of_sync: {
			label: () => m.statusOutOfSync(),
			className: "bg-amber-500/20 text-amber-700 dark:text-amber-400 border-amber-500/30",
			dotClass: "bg-amber-500 dark:bg-amber-400",
		},
		syncing: {
			label: () => m.statusSyncing(),
			className: "bg-blue-500/20 text-blue-700 dark:text-blue-400 border-blue-500/30",
			dotClass: "bg-blue-500 dark:bg-blue-400 animate-pulse",
		},
		unknown: {
			label: () => m.unknown(),
			className: "bg-zinc-500/20 text-zinc-600 dark:text-zinc-400 border-zinc-500/30",
			dotClass: "bg-zinc-400",
		},
	};

const healthConfig: Record<
	HealthStatus,
	{ label: () => string; className: string; dotClass: string }
> = {
	healthy: {
		label: () => m.statusHealthy(),
		className: "bg-emerald-500/20 text-emerald-700 dark:text-emerald-400 border-emerald-500/30",
		dotClass: "bg-emerald-500 dark:bg-emerald-400",
	},
	unhealthy: {
		label: () => m.statusUnhealthy(),
		className: "bg-red-500/20 text-red-700 dark:text-red-400 border-red-500/30",
		dotClass: "bg-red-500 dark:bg-red-400",
	},
	stopped: {
		label: () => m.statusStopped(),
		className: "bg-red-500/20 text-red-700 dark:text-red-400 border-red-500/30",
		dotClass: "bg-red-500 dark:bg-red-400",
	},
	degraded: {
		label: () => m.statusDegraded(),
		className: "bg-amber-500/20 text-amber-700 dark:text-amber-400 border-amber-500/30",
		dotClass: "bg-amber-500 dark:bg-amber-400",
	},
	unknown: {
		label: () => m.unknown(),
		className: "bg-zinc-500/20 text-zinc-600 dark:text-zinc-400 border-zinc-500/30",
		dotClass: "bg-zinc-400",
	},
};

export function getSyncStatusLabel(status: SyncStatus): string {
	return syncConfig[status].label();
}

export function getHealthStatusLabel(status: HealthStatus): string {
	return healthConfig[status].label();
}

export function ApplicationStatusBadge(props: StatusBadgeProps) {
	const config =
		props.type === "sync"
			? syncConfig[props.status as SyncStatus]
			: healthConfig[props.status as HealthStatus];

	return (
		<Badge variant="outline" className={cn("px-2.5 py-1", config.className)}>
			<span className={cn("h-1.5 w-1.5 rounded-full", config.dotClass)} />
			{config.label()}
		</Badge>
	);
}
