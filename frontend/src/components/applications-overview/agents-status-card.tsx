import { Link } from "@tanstack/react-router";
import { CircleCheck } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { AgentStatus, type Agent } from "@/lib/agents";
import { m } from "@/lib/paraglide/messages";
import { cn, formatRelativeTime } from "@/lib/utils";

const maxListedOfflineAgents = 2;

export function AgentsStatusCard({ agents }: { agents: Agent[] | undefined }) {
	const online = agents?.filter((agent) => agent.status === AgentStatus.Online).length ?? 0;
	const total = agents?.length ?? 0;
	const offlineAgents = agents?.filter((agent) => agent.status !== AgentStatus.Online) ?? [];

	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.pageAgents()}</CardTitle>
				<CardDescription>{m.applicationsAgentsDescription()}</CardDescription>
			</CardHeader>
			<CardContent className="space-y-3">
				{agents ? (
					<>
						<div className="flex items-baseline gap-2">
							<span
								className={cn(
									"text-2xl font-bold",
									offlineAgents.length > 0 && (online === 0 ? "text-red-400" : "text-amber-400"),
								)}
							>
								{online}
							</span>
							<span className="text-xs text-muted-foreground">
								{m.applicationsAgentsOnline({ total })}
							</span>
						</div>
						{offlineAgents.length === 0 ? (
							<p className="flex items-center gap-1.5 text-xs text-emerald-400">
								<CircleCheck className="size-3.5" />
								{m.applicationsAgentsAllConnected()}
							</p>
						) : (
							<ul className="space-y-1 text-xs text-muted-foreground">
								{offlineAgents.slice(0, maxListedOfflineAgents).map((agent) => (
									<li key={agent.id}>
										<Link to="/agents" className="block truncate hover:text-foreground">
											<span className="font-medium text-foreground">{agent.name}</span>
											{" · "}
											{agent.lastSeen
												? m.applicationsAgentsLastSeen({ time: formatRelativeTime(agent.lastSeen) })
												: m.applicationsAgentsNeverSeen()}
											{agent.appsCount
												? ` · ${m.applicationsAgentsAffectedApps({ count: agent.appsCount })}`
												: null}
										</Link>
									</li>
								))}
								{offlineAgents.length > maxListedOfflineAgents ? (
									<li>
										{m.applicationsAgentsMoreOffline({
											count: offlineAgents.length - maxListedOfflineAgents,
										})}
									</li>
								) : null}
							</ul>
						)}
					</>
				) : (
					<>
						<Skeleton className="h-8 w-20" />
						<Skeleton className="h-4 w-3/4" />
					</>
				)}
			</CardContent>
		</Card>
	);
}
