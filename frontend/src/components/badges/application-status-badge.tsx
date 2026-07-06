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
			className: "bg-emerald-500/20 text-emerald-400 border-emerald-500/30",
			dotClass: "bg-emerald-400",
		},
		out_of_sync: {
			label: () => m.statusOutOfSync(),
			className: "bg-amber-500/20 text-amber-400 border-amber-500/30",
			dotClass: "bg-amber-400",
		},
		syncing: {
			label: () => m.statusSyncing(),
			className: "bg-blue-500/20 text-blue-400 border-blue-500/30",
			dotClass: "bg-blue-400 animate-pulse",
		},
		unknown: {
			label: () => m.unknown(),
			className: "bg-zinc-500/20 text-zinc-400 border-zinc-500/30",
			dotClass: "bg-zinc-400",
		},
	};

const healthConfig: Record<
	HealthStatus,
	{ label: () => string; className: string; dotClass: string }
> = {
	healthy: {
		label: () => m.statusHealthy(),
		className: "bg-emerald-500/20 text-emerald-400 border-emerald-500/30",
		dotClass: "bg-emerald-400",
	},
	unhealthy: {
		label: () => m.statusUnhealthy(),
		className: "bg-amber-500/20 text-amber-400 border-amber-500/30",
		dotClass: "bg-amber-400",
	},
	unknown: {
		label: () => m.unknown(),
		className: "bg-zinc-500/20 text-zinc-400 border-zinc-500/30",
		dotClass: "bg-zinc-400",
	},
};

export function ApplicationStatusBadge(props: StatusBadgeProps) {
	let config =
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
