import type { ColumnDef } from "@tanstack/react-table";
import {
	canRollBackTo,
	eventSourceLabel,
	eventTitle,
	isCurrentDeployment,
	type ApplicationEvent,
} from "@/lib/application-events";
import { m } from "@/lib/paraglide/messages";
import { getLocale } from "@/lib/paraglide/runtime";
import { ApplicationEventStatusBadge } from "@/components/badges/application-event-status-badge";
import { RollbackButton } from "@/components/applications/rollback-button";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChevronDown, ChevronUp } from "lucide-react";
import { applicationEventsTableFeatures } from "./table-features";

export const getColumns = (
	applicationId: string,
	currentDeploymentId?: string | null,
): ColumnDef<typeof applicationEventsTableFeatures, ApplicationEvent>[] => [
	{
		accessorKey: "createdAt",
		header: () => m.applicationHistoryTimestamp(),
		cell: ({ row }) => {
			const date = new Date(row.original.createdAt);
			if (isNaN(date.getTime())) {
				return <span className="text-muted-foreground">-</span>;
			}
			return (
				<time dateTime={date.toISOString()} className="text-sm whitespace-nowrap">
					{date.toLocaleString(getLocale())}
				</time>
			);
		},
	},
	{
		accessorKey: "type",
		header: () => m.applicationHistoryTrigger(),
		cell: ({ row }) => (
			<div>
				<div className="flex items-center gap-2 font-medium">
					{eventTitle(row.original)}
					{isCurrentDeployment(row.original, currentDeploymentId) ? (
						<Badge variant="outline" className="border-blue-500/40 text-blue-500">
							{m.historyCurrentDeployment()}
						</Badge>
					) : null}
				</div>
				<span className="text-xs text-muted-foreground">
					{eventSourceLabel(row.original.source)}
				</span>
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
			return <span className="text-muted-foreground">{eventSourceLabel(source)}</span>;
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
				<ApplicationEventStatusBadge
					status={row.original.status}
					type={row.original.type}
					deploymentStatus={row.original.deploymentStatus}
				/>
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
	{
		id: "actions",
		header: () => null,
		cell: ({ row }) =>
			canRollBackTo(row.original, currentDeploymentId) ? (
				<div className="flex justify-end">
					<RollbackButton applicationId={applicationId} event={row.original} />
				</div>
			) : null,
	},
];
