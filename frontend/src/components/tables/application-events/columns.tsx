import type { ColumnDef } from "@tanstack/react-table";
import {
	ApplicationEventSource,
	ApplicationEventType,
	type ApplicationEvent,
} from "@/lib/application-events";
import { m } from "@/lib/paraglide/messages";
import { ApplicationEventStatusBadge } from "@/components/badges/application-event-status-badge";
import { Button } from "@/components/ui/button";
import { ChevronDown, ChevronUp } from "lucide-react";

const typeLabels: Record<ApplicationEventType, () => string> = {
	[ApplicationEventType.Deployment]: () => m.eventTypeDeployment(),
	[ApplicationEventType.CommitSync]: () => m.eventTypeCommitSync(),
	[ApplicationEventType.ImageUpdate]: () => m.eventTypeImageUpdate(),
};

const sourceLabels: Record<ApplicationEventSource, () => string> = {
	[ApplicationEventSource.Manual]: () => m.eventSourceManual(),
	[ApplicationEventSource.ApplicationCreated]: () => m.eventSourceApplicationCreated(),
	[ApplicationEventSource.RepositoryPolling]: () => m.eventSourceRepositoryPolling(),
	[ApplicationEventSource.RepositoryWebhook]: () => m.eventSourceRepositoryWebhook(),
	[ApplicationEventSource.GitHubActions]: () => m.eventSourceGitHubActions(),
	[ApplicationEventSource.ImagePolling]: () => m.eventSourceImagePolling(),
	[ApplicationEventSource.ImageWebhook]: () => m.eventSourceImageWebhook(),
};

function typeLabel(type: ApplicationEventType): string {
	return typeLabels[type]?.() ?? type;
}

function sourceLabel(source: ApplicationEventSource): string {
	return sourceLabels[source]?.() ?? source;
}

export const columns: ColumnDef<ApplicationEvent>[] = [
	{
		accessorKey: "createdAt",
		header: () => m.applicationHistoryTimestamp(),
		cell: ({ row }) => {
			const date = new Date(row.original.createdAt);
			if (isNaN(date.getTime())) {
				return <span className="text-muted-foreground">-</span>;
			}
			return <span className="text-sm whitespace-nowrap">{date.toLocaleString()}</span>;
		},
	},
	{
		accessorKey: "type",
		header: () => m.applicationHistoryTrigger(),
		cell: ({ row }) => (
			<div>
				<div className="font-medium">{typeLabel(row.original.type)}</div>
				<span className="text-xs text-muted-foreground">{sourceLabel(row.original.source)}</span>
			</div>
		),
	},
	{
		accessorKey: "actorName",
		header: () => m.applicationHistoryTriggeredBy(),
		cell: ({ row }) => {
			const { actorName, source } = row.original;
			if (actorName) {
				return <span>{actorName}</span>;
			}
			return <span className="text-muted-foreground">{sourceLabel(source)}</span>;
		},
	},
	{
		accessorKey: "commitHash",
		header: () => m.applicationHistoryCommit(),
		cell: ({ row }) => {
			const { commitHash, commitMessage } = row.original;
			if (!commitHash) {
				return <span className="text-muted-foreground">-</span>;
			}
			return (
				<div className="min-w-0">
					<span className="font-mono text-xs select-all">{commitHash.slice(0, 7)}</span>
					{commitMessage && (
						<p className="text-xs text-muted-foreground truncate max-w-64">{commitMessage}</p>
					)}
				</div>
			);
		},
	},
	{
		accessorKey: "status",
		header: () => m.applicationHistoryResult(),
		cell: ({ row }) => (
			<div className="flex items-center gap-1">
				<ApplicationEventStatusBadge status={row.original.status} />
				{row.original.errorMessage && (
					<Button
						variant="ghost"
						size="icon-sm"
						aria-label={m.applicationHistoryErrorDetails()}
						onClick={row.getToggleExpandedHandler()}
					>
						{row.getIsExpanded() ? (
							<ChevronUp className="h-4 w-4" />
						) : (
							<ChevronDown className="h-4 w-4" />
						)}
					</Button>
				)}
			</div>
		),
	},
];
