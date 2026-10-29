import { Link } from "@tanstack/react-router";
import { ArrowUpRight, Server } from "lucide-react";
import {
	getHealthStatusLabel,
	getSyncStatusLabel,
} from "@/components/badges/application-status-badge";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import { HealthStatus, SyncStatus, type ApplicationListItem } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { ResourceCard, type FlowState } from "./resource-card";

const syncState: Record<SyncStatus, FlowState> = {
	[SyncStatus.Synced]: "ok",
	[SyncStatus.OutOfSync]: "warning",
	[SyncStatus.Syncing]: "progress",
	[SyncStatus.Unknown]: "unknown",
};

const healthState: Record<HealthStatus, FlowState> = {
	[HealthStatus.Healthy]: "ok",
	[HealthStatus.Unhealthy]: "warning",
	[HealthStatus.Degraded]: "warning",
	[HealthStatus.Stopped]: "error",
	[HealthStatus.Unknown]: "unknown",
};

export function ApplicationCard({ app }: { app: ApplicationListItem }) {
	return (
		<Link
			to="/applications/$id"
			params={{ id: app.id }}
			className="group block h-full rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-ring"
		>
			<ResourceCard
				icon={<StaticLucideIcon name={app.icon} className="size-5 text-primary" />}
				title={app.name}
				subtitle={`${app.repositoryName} / ${app.branch}`}
				monoSubtitle
				action={
					<ArrowUpRight className="m-1.5 size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:-translate-y-0.5 group-hover:text-primary" />
				}
				stages={[
					{
						label: m.cardStageRevision(),
						value: app.commit ? app.commit.slice(0, 7) : m.notAvailableShort(),
						title: app.commit || undefined,
						state: app.commit ? "info" : "unknown",
						mono: true,
					},
					{
						label: m.cardStageSync(),
						value: getSyncStatusLabel(app.syncStatus),
						state: syncState[app.syncStatus],
					},
					{
						label: m.cardStageHealth(),
						value: getHealthStatusLabel(app.healthStatus),
						state: healthState[app.healthStatus],
					},
				]}
				error={app.lastSyncError}
				meta={
					<>
						<Server className="size-3.5 shrink-0" />
						<span className="truncate">{app.agentName}</span>
					</>
				}
				time={app.lastSyncedAt ?? null}
			/>
		</Link>
	);
}
