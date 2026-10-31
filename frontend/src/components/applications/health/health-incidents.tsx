import { RelativeTime } from "@/components/relative-time";
import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import {
	formatHealthDuration,
	HealthChangeReason,
	healthStatusConfig,
	type HealthIncident,
	type HealthStats,
} from "@/lib/application-health";
import { ApplicationEventType } from "@/lib/application-events";
import { HealthStatus } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";

const PLACEHOLDER = "\u0000";

// Renders a translated message with its placeholder value emphasized, without
// splitting the sentence into separately translated parts.
function withHighlight(message: (value: string) => string, value: ReactNode): ReactNode {
	const [before, after] = message(PLACEHOLDER).split(PLACEHOLDER);
	return (
		<>
			{before}
			{value}
			{after}
		</>
	);
}

function IncidentContext({ incident, agentName }: { incident: HealthIncident; agentName: string }) {
	const parts: ReactNode[] = [];

	if (incident.status === HealthStatus.Unknown) {
		parts.push(
			incident.reason === HealthChangeReason.AgentOffline
				? withHighlight(
						(agent) => m.healthIncidentAgentOffline({ agent }),
						<span className="font-medium text-foreground">{agentName}</span>,
					)
				: m.healthIncidentUnknown(),
		);
	}

	if (incident.trigger?.type === ApplicationEventType.Deployment) {
		parts.push(
			incident.trigger.commitHash
				? withHighlight(
						(commit) => m.healthIncidentAfterDeployCommit({ commit }),
						<span className="font-mono text-xs text-foreground">
							{incident.trigger.commitHash.slice(0, 7)}
						</span>,
					)
				: m.healthIncidentAfterDeploy(),
		);
	} else if (incident.trigger?.type === ApplicationEventType.ImageUpdate) {
		parts.push(m.healthIncidentAfterImageUpdate());
	}

	// Stopped means every container is stopped, so listing the services adds nothing.
	if (incident.status === HealthStatus.Stopped) {
		parts.push(m.applicationsAttentionStopped());
	} else if (incident.services.length > 0) {
		parts.push(
			<span className="inline-flex flex-wrap items-center gap-1.5">
				{incident.services.map((service) => (
					<Badge key={service.name} variant="outline" className="gap-1.5 font-normal">
						<span
							className={cn("size-1.5 rounded-full", healthStatusConfig[service.status].className)}
						/>
						{service.name}
						<span className="text-muted-foreground">
							{healthStatusConfig[service.status].label()}
						</span>
					</Badge>
				))}
			</span>,
		);
	}

	if (parts.length === 0) {
		return <span className="text-muted-foreground">–</span>;
	}
	return (
		<div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
			{parts.map((part, index) => (
				<span key={index}>{part}</span>
			))}
		</div>
	);
}

export function HealthIncidents({ stats, agentName }: { stats: HealthStats; agentName: string }) {
	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.healthIncidents()}</CardTitle>
				<CardAction className="text-sm text-muted-foreground">
					{m.healthIncidentsSummary({
						incidents: stats.incidentCount,
						gaps: stats.dataGapCount,
						range: stats.range,
					})}
				</CardAction>
			</CardHeader>
			<CardContent>
				{stats.incidents.length === 0 ? (
					<div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
						{m.healthNoIncidents({ range: stats.range })}
					</div>
				) : (
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>{m.columnStatus()}</TableHead>
								<TableHead>{m.healthIncidentStarted()}</TableHead>
								<TableHead>{m.healthIncidentDuration()}</TableHead>
								<TableHead>{m.healthIncidentContext()}</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{stats.incidents.map((incident) => (
								<TableRow key={`${incident.status}-${incident.startedAt}`}>
									<TableCell>
										<span className="flex items-center gap-2">
											<span
												className={cn(
													"size-2.5 rounded-xs",
													healthStatusConfig[incident.status].className,
												)}
											/>
											{healthStatusConfig[incident.status].label()}
										</span>
									</TableCell>
									<TableCell className="tabular-nums">
										<RelativeTime value={incident.startedAt} />
									</TableCell>
									<TableCell className="tabular-nums">
										{formatHealthDuration(incident.durationSeconds)}
										{incident.endedAt === null ? (
											<Badge variant="outline" className="ml-2">
												{m.healthIncidentOngoing()}
											</Badge>
										) : null}
									</TableCell>
									<TableCell className="whitespace-normal">
										<IncidentContext incident={incident} agentName={agentName} />
									</TableCell>
								</TableRow>
							))}
						</TableBody>
					</Table>
				)}
			</CardContent>
		</Card>
	);
}
