import type { ReactNode } from "react";
import { getRepositorySyncStatusLabel } from "@/components/badges/repository-status-badge";
import { getSyncTypeIcon, getSyncTypeLabel } from "@/components/badges/repository-sync-type-badge";
import { m } from "@/lib/paraglide/messages";
import {
	getGitProviderIconClass,
	getGitProviderIconPath,
	type Repository,
	type RepositorySyncStatus,
} from "@/lib/repositories";
import { cn } from "@/lib/utils";
import { ResourceCard, type FlowState } from "./resource-card";

const syncState: Record<RepositorySyncStatus, FlowState> = {
	success: "ok",
	syncing: "progress",
	failed: "error",
	unknown: "unknown",
};

// Handles HTTP(S) as well as scp-like SSH URLs (git@host:org/repo.git).
function getRemoteHost(url: string): string {
	try {
		return new URL(url).host || url;
	} catch {
		return /^[^@]+@([^:/]+)/.exec(url)?.[1] ?? url;
	}
}

export function RepositoryCard({
	repository,
	action,
}: {
	repository: Repository;
	action?: ReactNode;
}) {
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
			action={action}
			stages={[
				{
					label: m.cardStageRemote(),
					value: getRemoteHost(repository.url),
					title: repository.url,
					state: "info",
					mono: true,
				},
				{
					label: m.cardStageSync(),
					value: getRepositorySyncStatusLabel(repository.syncStatus),
					state: syncState[repository.syncStatus],
				},
				{
					label: m.cardStageApplications(),
					value: repository.appCount ?? m.notAvailableShort(),
					state: repository.appCount ? "info" : "off",
				},
			]}
			error={repository.syncStatus === "failed" ? repository.lastSyncError : null}
			meta={
				<>
					{getSyncTypeIcon(repository.syncType, "size-3.5 shrink-0")}
					<span className="truncate">{getSyncTypeLabel(repository.syncType)}</span>
				</>
			}
			time={repository.lastSyncedAt}
		/>
	);
}
