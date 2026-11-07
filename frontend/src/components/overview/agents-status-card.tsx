import { RelativeTime } from "@/components/relative-time";
import { Link } from "@tanstack/react-router";
import { CircleCheck, Server } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { AgentStatus, type Agent } from "@/lib/agents";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";
import { Metric, OverviewCardHeader } from "./overview-card";

const maxListedAgents = 3;

const statusOrder: AgentStatus[] = [AgentStatus.Offline, AgentStatus.Error, AgentStatus.Online];

const statusDotClass: Record<AgentStatus, string> = {
	[AgentStatus.Online]: "bg-emerald-500 dark:bg-emerald-400",
	[AgentStatus.Offline]: "bg-red-500 dark:bg-red-400",
	[AgentStatus.Error]: "bg-amber-500 dark:bg-amber-400",
};

export function AgentsStatusCard({ agents }: { agents: Agent[] | undefined }) {
	const online = agents?.filter((agent) => agent.status === AgentStatus.Online).length ?? 0;
	const total = agents?.length ?? 0;
	// Offline agents and agents that cannot reach Docker both block deployments.
	const unavailable = total - online;
	// Agents that need attention first, so they are never cut off by the limit.
	const listed = [...(agents ?? [])]
		.sort(
			(a, b) =>
				statusOrder.indexOf(a.status) - statusOrder.indexOf(b.status) ||
				a.name.localeCompare(b.name),
		)
		.slice(0, maxListedAgents);

	return (
		<Card>
			<OverviewCardHeader
				icon={Server}
				title={m.pageAgents()}
				description={m.applicationsAgentsDescription()}
			/>
			<CardContent className="flex flex-col gap-3">
				{agents ? (
					<>
						<Metric
							value={online}
							label={m.applicationsAgentsOnline({ total })}
							tone={unavailable > 0 ? (online === 0 ? "error" : "warning") : "default"}
						/>
						{unavailable === 0 && total > 0 ? (
							<p className="flex items-center gap-1.5 text-xs text-emerald-700 dark:text-emerald-400">
								<CircleCheck className="size-3.5" />
								{m.applicationsAgentsAllConnected()}
							</p>
						) : null}
						{listed.length > 0 ? (
							<ul className="-mx-2">
								{listed.map((agent) => (
									<li
										key={agent.id}
										className="flex min-h-8 items-center gap-x-2 rounded-md px-2 py-1 text-sm transition-colors hover:bg-muted/60"
									>
										<span
											className={cn("size-2 shrink-0 rounded-full", statusDotClass[agent.status])}
										/>
										{/* The detail wraps below the name when space runs out instead of being cut off,
										    it is the reason the agent is listed. */}
										<div className="flex min-w-0 flex-1 items-baseline justify-between gap-x-3">
											<Link
												to="/agents"
												className="max-w-full truncate font-medium hover:underline"
											>
												{agent.name}
											</Link>
											<span className="min-w-0 truncate text-xs text-muted-foreground">
												<AgentStatusDetail agent={agent} />
											</span>
										</div>
									</li>
								))}
							</ul>
						) : null}
						{total > maxListedAgents ? (
							<Link to="/agents" className="text-xs text-muted-foreground hover:text-foreground">
								{m.applicationsAttentionMore({ count: total - maxListedAgents })}
							</Link>
						) : null}
					</>
				) : (
					<>
						<Skeleton className="h-8 w-20" />
						<Skeleton className="h-16 w-full" />
					</>
				)}
			</CardContent>
		</Card>
	);
}

function AgentStatusDetail({ agent }: { agent: Agent }) {
	if (agent.status === AgentStatus.Online) {
		return m.statusOnline();
	}
	const status =
		agent.status === AgentStatus.Error ? (
			m.applicationsAgentsDockerUnreachable()
		) : agent.lastSeen ? (
			<RelativeTime
				value={agent.lastSeen}
				format={(time) => m.applicationsAgentsLastSeen({ time })}
			/>
		) : (
			m.applicationsAgentsNeverSeen()
		);

	return (
		<>
			{status}
			{agent.appsCount
				? ` · ${agent.appsCount === 1 ? m.applicationsAgentsAffectedApp() : m.applicationsAgentsAffectedApps({ count: agent.appsCount })}`
				: null}
		</>
	);
}
