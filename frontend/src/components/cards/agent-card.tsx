import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { Container } from "lucide-react";
import { AgentVersionBadge } from "@/components/badges/agent-version-badge";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import { Skeleton } from "@/components/ui/skeleton";
import { AgentStatus, type Agent } from "@/lib/agents";
import type { ApplicationListItem } from "@/lib/applications";
import { fleetCategoryConfig, getFleetCategory } from "@/lib/fleet";
import { m } from "@/lib/paraglide/messages";
import { cn, formatRelativeTime } from "@/lib/utils";
import { ResourceCard } from "./resource-card";

const maxListedApplications = 3;

function ConnectionStatus({ agent }: { agent: Agent }) {
	if (agent.status === AgentStatus.Offline) {
		return (
			<span className="flex items-center gap-1.5">
				<span className="size-2 shrink-0 rounded-full bg-red-500" />
				<span className="text-red-600 dark:text-red-400">{m.statusOffline()}</span>
				<span className="truncate">
					·{" "}
					{agent.lastSeen
						? m.applicationsAgentsLastSeen({ time: formatRelativeTime(agent.lastSeen) })
						: m.applicationsAgentsNeverSeen()}
				</span>
			</span>
		);
	}

	// An agent in the error state is connected to the hub, but cannot reach the Docker daemon.
	const dockerUnreachable = agent.status === AgentStatus.Error;
	return (
		<span
			className="flex items-center gap-1.5"
			title={dockerUnreachable ? m.agentStatusErrorDescription() : undefined}
		>
			<span className="relative flex size-2 shrink-0">
				{dockerUnreachable ? null : (
					<span className="absolute inline-flex size-full rounded-full bg-emerald-500 opacity-60 motion-safe:animate-ping" />
				)}
				<span
					className={cn(
						"relative inline-flex size-2 rounded-full",
						dockerUnreachable ? "bg-amber-500" : "bg-emerald-500",
					)}
				/>
			</span>
			<span className="text-emerald-600 dark:text-emerald-400">{m.statusOnline()}</span>
			{dockerUnreachable ? (
				<span className="truncate text-amber-600 dark:text-amber-400">
					· {m.applicationsAgentsDockerUnreachable()}
				</span>
			) : null}
		</span>
	);
}

function ApplicationList({
	agentId,
	apps,
}: {
	agentId: string;
	apps: ApplicationListItem[] | undefined;
}) {
	if (!apps) {
		return (
			<div className="space-y-2">
				<Skeleton className="h-7 w-full" />
				<Skeleton className="h-7 w-2/3" />
			</div>
		);
	}

	if (apps.length === 0) {
		return (
			<p className="rounded-lg border border-dashed px-3 py-2.5 text-center text-xs text-muted-foreground">
				{m.cardAgentNoApplications()}
			</p>
		);
	}

	return (
		<ul className="-mx-2">
			{apps.slice(0, maxListedApplications).map((app) => {
				const category = fleetCategoryConfig[getFleetCategory(app)];
				return (
					<li key={app.id}>
						<Link
							to="/applications/$id"
							params={{ id: app.id }}
							className="flex items-center gap-2.5 rounded-md px-2 py-1.5 outline-none hover:bg-muted/60 focus-visible:ring-2 focus-visible:ring-ring"
						>
							<StaticLucideIcon name={app.icon} className="size-4 shrink-0 text-muted-foreground" />
							<span className="min-w-0 flex-1 truncate">{app.name}</span>
							<span className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground">
								{category.label()}
								<span className={cn("size-1.5 rounded-full", category.dotClass)} />
							</span>
						</Link>
					</li>
				);
			})}
			{apps.length > maxListedApplications ? (
				<li>
					<Link
						to="/applications"
						search={{ agent: agentId }}
						className="block px-2 pt-1 text-xs text-muted-foreground hover:text-foreground"
					>
						{m.applicationsAttentionMore({ count: apps.length - maxListedApplications })}
					</Link>
				</li>
			) : null}
		</ul>
	);
}

export function AgentCard({
	agent,
	apps,
	action,
}: {
	agent: Agent;
	apps: ApplicationListItem[] | undefined;
	action?: ReactNode;
}) {
	return (
		<ResourceCard
			icon={<StaticLucideIcon name={agent.icon} className="size-5 text-primary" />}
			title={agent.name}
			subtitle={<ConnectionStatus agent={agent} />}
			action={action}
			footer={
				<>
					<span className="flex min-w-0 items-center gap-1.5" title={m.dockerVersion()}>
						<Container className="size-3.5 shrink-0" />
						<span className="truncate">
							Docker{" "}
							<span className="font-mono">{agent.dockerVersion ?? m.notAvailableShort()}</span>
						</span>
					</span>
					<AgentVersionBadge version={agent.version} status={agent.versionStatus} />
				</>
			}
		>
			<div className="mb-1.5 flex items-baseline justify-between text-[0.6875rem] font-medium tracking-wider text-muted-foreground uppercase">
				<span>{m.navApplications()}</span>
				{apps ? <span className="tabular-nums">{apps.length}</span> : null}
			</div>
			<ApplicationList agentId={agent.id} apps={apps} />
		</ResourceCard>
	);
}
