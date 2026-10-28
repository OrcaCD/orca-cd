import { AgentStatus, type Agent } from "@/lib/agents";
import { HealthStatus, SyncStatus, type ApplicationListItem } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";

export type FleetCategory = "ok" | "syncing" | "outOfSync" | "unhealthy" | "unknown";

// Ordered by severity so the attention list and the status bar show the most
// pressing problems first.
export const fleetCategories: FleetCategory[] = [
	"unhealthy",
	"outOfSync",
	"syncing",
	"unknown",
	"ok",
];

export const fleetCategoryConfig: Record<
	FleetCategory,
	{ label: () => string; dotClass: string; barClass: string }
> = {
	ok: {
		label: () => m.applicationsFleetOk(),
		dotClass: "bg-emerald-400",
		barClass: "bg-emerald-500",
	},
	syncing: {
		label: () => m.statusSyncing(),
		dotClass: "bg-blue-400 animate-pulse",
		barClass: "bg-blue-500",
	},
	outOfSync: {
		label: () => m.statusOutOfSync(),
		dotClass: "bg-amber-400",
		barClass: "bg-amber-500",
	},
	unhealthy: {
		label: () => m.statusUnhealthy(),
		dotClass: "bg-red-400",
		barClass: "bg-red-500",
	},
	unknown: {
		label: () => m.unknown(),
		dotClass: "bg-zinc-400",
		barClass: "bg-zinc-500",
	},
};

export function getFleetCategory(app: ApplicationListItem): FleetCategory {
	// Stopped and degraded apps are (partially) not running, so they need the
	// same attention.
	if (
		app.healthStatus === HealthStatus.Unhealthy ||
		app.healthStatus === HealthStatus.Stopped ||
		app.healthStatus === HealthStatus.Degraded
	) {
		return "unhealthy";
	}
	if (app.syncStatus === SyncStatus.OutOfSync) {
		return "outOfSync";
	}
	if (app.syncStatus === SyncStatus.Syncing) {
		return "syncing";
	}
	if (app.syncStatus === SyncStatus.Synced && app.healthStatus === HealthStatus.Healthy) {
		return "ok";
	}
	return "unknown";
}

export function countFleetCategories(apps: ApplicationListItem[]): Record<FleetCategory, number> {
	const counts: Record<FleetCategory, number> = {
		ok: 0,
		syncing: 0,
		outOfSync: 0,
		unhealthy: 0,
		unknown: 0,
	};
	for (const app of apps) {
		counts[getFleetCategory(app)]++;
	}
	return counts;
}

export interface AttentionItem {
	app: ApplicationListItem;
	category: FleetCategory;
	reason: string;
	agentStatus: AgentStatus;
}

export function getAttentionItems(
	apps: ApplicationListItem[],
	agents: Agent[] | undefined,
): AttentionItem[] {
	// Until agents are loaded, treat them as online so apps are not flagged
	// as unreachable for a moment.
	const agentStatuses = new Map(agents?.map((agent) => [agent.id, agent.status]));

	const items: AttentionItem[] = [];
	for (const app of apps) {
		const category = getFleetCategory(app);
		const agentStatus = agentStatuses.get(app.agentId) ?? AgentStatus.Online;
		if (
			category !== "unhealthy" &&
			category !== "outOfSync" &&
			!app.lastSyncError &&
			agentStatus === AgentStatus.Online
		) {
			continue;
		}

		let reason: string;
		if (app.lastSyncError) {
			reason = app.lastSyncError;
		} else if (agentStatus === AgentStatus.Offline) {
			reason = m.applicationsAttentionAgentOffline({ agent: app.agentName });
		} else if (agentStatus === AgentStatus.Error) {
			reason = m.applicationsAttentionAgentError({ agent: app.agentName });
		} else if (app.healthStatus === HealthStatus.Stopped) {
			reason = m.applicationsAttentionStopped();
		} else if (app.healthStatus === HealthStatus.Degraded) {
			reason = m.applicationsAttentionDegraded();
		} else if (category === "unhealthy") {
			reason = m.applicationsAttentionUnhealthy();
		} else {
			reason = m.applicationsAttentionOutOfSync();
		}
		items.push({ app, category, reason, agentStatus });
	}

	return items.sort(
		(a, b) => fleetCategories.indexOf(a.category) - fleetCategories.indexOf(b.category),
	);
}
