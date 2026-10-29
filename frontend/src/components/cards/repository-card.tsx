import { useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { AppWindow, CircleCheck, CircleDashed, CircleX, RefreshCw } from "lucide-react";
import { getRepositorySyncStatusLabel } from "@/components/badges/repository-status-badge";
import { getSyncTypeIcon, getSyncTypeLabel } from "@/components/badges/repository-sync-type-badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { ApplicationListItem } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import {
	getGitProviderIconClass,
	getGitProviderIconPath,
	type Repository,
	type RepositorySyncStatus,
} from "@/lib/repositories";
import { cn } from "@/lib/utils";
import { RelativeTime, ResourceCard } from "./resource-card";

const maxListedApplications = 2;

const syncStatusIcon: Record<RepositorySyncStatus, ReactNode> = {
	success: <CircleCheck className="size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />,
	failed: <CircleX className="size-4 shrink-0 text-red-600 dark:text-red-400" />,
	syncing: (
		<RefreshCw className="size-4 shrink-0 text-blue-600 motion-safe:animate-spin dark:text-blue-400" />
	),
	unknown: <CircleDashed className="size-4 shrink-0 text-muted-foreground" />,
};

function formatInterval(seconds: number): string {
	if (seconds % 3600 === 0) {
		return `${seconds / 3600}h`;
	}
	if (seconds % 60 === 0) {
		return `${seconds / 60}m`;
	}
	return `${seconds}s`;
}

function getTriggerLabel(repository: Repository): string {
	if (repository.syncType === "polling" && repository.pollingIntervalSeconds) {
		return m.cardPollingEvery({ interval: formatInterval(repository.pollingIntervalSeconds) });
	}
	return getSyncTypeLabel(repository.syncType);
}

function UsedBy({ apps }: { apps: ApplicationListItem[] | undefined }) {
	if (!apps) {
		return <Skeleton className="h-4 w-32" />;
	}
	if (apps.length === 0) {
		return <span className="truncate">{m.cardRepositoryUnused()}</span>;
	}

	return (
		<span className="flex min-w-0 items-center gap-1.5">
			<AppWindow className="size-3.5 shrink-0" />
			{apps.slice(0, maxListedApplications).map((app) => (
				<Link
					key={app.id}
					to="/applications/$id"
					params={{ id: app.id }}
					className="min-w-0 truncate rounded-md border bg-background px-1.5 py-0.5 text-foreground hover:border-primary/50"
				>
					{app.name}
				</Link>
			))}
			{apps.length > maxListedApplications ? (
				<span className="shrink-0">
					{m.applicationsAttentionMore({ count: apps.length - maxListedApplications })}
				</span>
			) : null}
		</span>
	);
}

export function RepositoryCard({
	repository,
	apps,
	onSync,
	action,
}: {
	repository: Repository;
	apps: ApplicationListItem[] | undefined;
	onSync: () => Promise<void>;
	action?: ReactNode;
}) {
	const [triggering, setTriggering] = useState(false);
	const failed = repository.syncStatus === "failed";

	const handleSync = async () => {
		setTriggering(true);
		try {
			await onSync();
		} finally {
			setTriggering(false);
		}
	};

	return (
		<ResourceCard
			icon={
				<img
					src={getGitProviderIconPath(repository.provider)}
					alt={m.gitProviderAlt()}
					className={cn("size-6", getGitProviderIconClass(repository.provider))}
				/>
			}
			title={repository.name}
			subtitle={<span title={repository.url}>{repository.url.replace(/^[a-z]+:\/\//, "")}</span>}
			monoSubtitle
			action={action}
			footer={<UsedBy apps={apps} />}
		>
			<div
				className={cn(
					"rounded-lg border bg-muted/30",
					failed && "border-red-500/30 bg-red-500/5 dark:bg-red-500/10",
				)}
			>
				<div className="px-3 pt-2.5 pb-2">
					<div className="flex items-center gap-2">
						{syncStatusIcon[repository.syncStatus]}
						<span className="font-medium">
							{getRepositorySyncStatusLabel(repository.syncStatus)}
						</span>
						<RelativeTime
							value={repository.lastSyncedAt}
							className="ml-auto text-xs text-muted-foreground"
						/>
					</div>
					{failed && repository.lastSyncError ? (
						<p
							className="mt-1 line-clamp-2 pl-6 text-xs text-red-600 dark:text-red-400"
							title={repository.lastSyncError}
						>
							{repository.lastSyncError}
						</p>
					) : null}
				</div>
				<div className="flex items-center gap-1.5 border-t px-3 py-1.5 text-xs text-muted-foreground">
					{getSyncTypeIcon(repository.syncType, "size-3.5 shrink-0")}
					<span className="truncate">{getTriggerLabel(repository)}</span>
					<Button
						variant="ghost"
						size="xs"
						className="-mr-2 ml-auto"
						disabled={triggering || repository.syncStatus === "syncing"}
						onClick={() => void handleSync()}
					>
						<RefreshCw className={cn(triggering && "motion-safe:animate-spin")} />
						{m.sync()}
					</Button>
				</div>
			</div>
		</ResourceCard>
	);
}
