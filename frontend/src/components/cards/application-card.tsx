import { Link } from "@tanstack/react-router";
import { ArrowUpRight, Server } from "lucide-react";
import {
	getHealthStatusLabel,
	getSyncStatusLabel,
} from "@/components/badges/application-status-badge";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import { RelativeTime } from "@/components/relative-time";
import { HealthStatus, SyncStatus, type ApplicationListItem } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { CardError, ResourceCard, ResourceFlow, type FlowState } from "./resource-card";

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
		// The link overlays the card instead of wrapping it, as interactive content like the timestamp
		// tooltip must not be nested inside a link.
		<div className="group/application relative h-full">
			<Link
				to="/applications/$id"
				params={{ id: app.id }}
				aria-label={app.name}
				className="absolute inset-0 rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-ring"
			/>
			<ResourceCard
				className="group-hover/application:ring-primary/50"
				icon={<StaticLucideIcon name={app.icon} className="size-5 text-primary" />}
				title={app.name}
				subtitle={`${app.repositoryName} @ ${app.branch}`}
				monoSubtitle
				action={
					<ArrowUpRight className="m-1.5 size-4 text-muted-foreground transition-transform group-hover/application:translate-x-0.5 group-hover/application:-translate-y-0.5 group-hover/application:text-primary" />
				}
				footer={
					<>
						<span className="flex min-w-0 items-center gap-1.5">
							<Server className="size-3.5 shrink-0" />
							<span className="truncate">{app.agentName}</span>
						</span>
						<RelativeTime value={app.lastSyncedAt} className="relative" />
					</>
				}
			>
				<ResourceFlow
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
				/>
				{app.lastSyncError ? <CardError message={app.lastSyncError} className="mt-3" /> : null}
			</ResourceCard>
		</div>
	);
}
