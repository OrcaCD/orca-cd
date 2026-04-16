import type { HealthStatus, SyncStatus } from "@/lib/applications";
import { cn } from "@/lib/utils";

type StatusBadgeProps =
	| { type: "sync"; status: SyncStatus }
	| { type: "health"; status: HealthStatus };

const syncConfig: Record<SyncStatus, { label: string; className: string; dotClass: string }> = {
	Synced: {
		label: "Synced",
		className: "bg-emerald-500/20 text-emerald-400 border-emerald-500/30",
		dotClass: "bg-emerald-400",
	},
	OutOfSync: {
		label: "Out of Sync",
		className: "bg-amber-500/20 text-amber-400 border-amber-500/30",
		dotClass: "bg-amber-400",
	},
	Progressing: {
		label: "Syncing",
		className: "bg-blue-500/20 text-blue-400 border-blue-500/30",
		dotClass: "bg-blue-400 animate-pulse",
	},
	Unknown: {
		label: "Unknown",
		className: "bg-zinc-500/20 text-zinc-400 border-zinc-500/30",
		dotClass: "bg-zinc-400",
	},
};

const healthConfig: Record<HealthStatus, { label: string; className: string; dotClass: string }> = {
	Healthy: {
		label: "Healthy",
		className: "bg-emerald-500/20 text-emerald-400 border-emerald-500/30",
		dotClass: "bg-emerald-400",
	},
	Degraded: {
		label: "Degraded",
		className: "bg-amber-500/20 text-amber-400 border-amber-500/30",
		dotClass: "bg-amber-400",
	},
	Progressing: {
		label: "Unhealthy",
		className: "bg-red-500/20 text-red-400 border-red-500/30",
		dotClass: "bg-red-400",
	},
	Unknown: {
		label: "Unknown",
		className: "bg-zinc-500/20 text-zinc-400 border-zinc-500/30",
		dotClass: "bg-zinc-400",
	},
};

export function StatusBadge(props: StatusBadgeProps) {
	let config =
		props.type === "sync"
			? syncConfig[props.status as SyncStatus]
			: healthConfig[props.status as HealthStatus];

	return (
		<span
			className={cn(
				"inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border",
				config.className,
			)}
		>
			<span className={cn("h-1.5 w-1.5 rounded-full", config.dotClass)} />
			{config.label}
		</span>
	);
}
