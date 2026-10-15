import { Link } from "@tanstack/react-router";
import { CircleCheck, RefreshCw } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
	Item,
	ItemActions,
	ItemContent,
	ItemDescription,
	ItemGroup,
	ItemTitle,
} from "@/components/ui/item";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/toast";
import { useFetch } from "@/lib/api";
import { m } from "@/lib/paraglide/messages";
import { syncRepository, type Repository, type RepositorySyncStatus } from "@/lib/repositories";
import { cn, formatRelativeTime } from "@/lib/utils";

const maxListedRepositories = 4;

const syncStatusDotClass: Record<RepositorySyncStatus, string> = {
	failed: "bg-red-400",
	syncing: "bg-blue-400 animate-pulse",
	success: "bg-emerald-400",
	unknown: "bg-zinc-400",
};

// Failed repositories first, as they are the ones that need an action.
const syncStatusOrder: RepositorySyncStatus[] = ["failed", "syncing", "unknown", "success"];

export function RepositoriesCard() {
	const { data: repositories } = useFetch<Repository[]>("/repositories");

	const synced = repositories?.filter((repo) => repo.syncStatus === "success").length ?? 0;
	const total = repositories?.length ?? 0;
	const failed = repositories?.filter((repo) => repo.syncStatus === "failed").length ?? 0;
	const listed = [...(repositories ?? [])]
		.sort(
			(a, b) =>
				syncStatusOrder.indexOf(a.syncStatus) - syncStatusOrder.indexOf(b.syncStatus) ||
				a.name.localeCompare(b.name),
		)
		.slice(0, maxListedRepositories);

	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.navRepositories()}</CardTitle>
				<CardDescription>{m.overviewRepositoriesDescription()}</CardDescription>
			</CardHeader>
			<CardContent className="space-y-3">
				{repositories ? (
					<>
						<div className="flex items-baseline gap-2">
							<span className={cn("text-2xl font-bold", failed > 0 && "text-red-400")}>
								{synced}
							</span>
							<span className="text-xs text-muted-foreground">
								{m.overviewRepositoriesSynced({ total })}
							</span>
						</div>
						{total > 0 && synced === total ? (
							<p className="flex items-center gap-1.5 text-xs text-emerald-400">
								<CircleCheck className="size-3.5" />
								{m.overviewRepositoriesAllSynced()}
							</p>
						) : null}
						{listed.length > 0 ? (
							<ItemGroup className="gap-0">
								{listed.map((repo) => (
									<RepositoryRow key={repo.id} repo={repo} />
								))}
							</ItemGroup>
						) : null}
						{total > maxListedRepositories ? (
							<Link
								to="/repositories"
								className="block px-2.5 text-xs text-muted-foreground hover:text-foreground"
							>
								{m.applicationsAttentionMore({ count: total - maxListedRepositories })}
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

function RepositoryRow({ repo }: { repo: Repository }) {
	const [syncing, setSyncing] = useState(false);

	const handleSync = async () => {
		setSyncing(true);
		try {
			await syncRepository(repo.id);
			toast.add({ type: "success", title: m.syncTriggered() });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedTriggerSync(),
			});
		} finally {
			setSyncing(false);
		}
	};

	const detail =
		repo.syncStatus === "failed" && repo.lastSyncError
			? repo.lastSyncError
			: repo.lastSyncedAt
				? m.overviewRepositoriesLastSynced({ time: formatRelativeTime(repo.lastSyncedAt) })
				: m.never();

	return (
		<Item size="xs" className="hover:bg-muted/40">
			<span className={cn("size-1.5 shrink-0 rounded-full", syncStatusDotClass[repo.syncStatus])} />
			<ItemContent className="min-w-0">
				<ItemTitle className="w-full">
					<Link
						to="/repositories/$id"
						params={{ id: repo.id }}
						className="truncate hover:underline"
					>
						{repo.name}
					</Link>
				</ItemTitle>
				<ItemDescription
					className={cn("truncate text-xs", repo.syncStatus === "failed" && "text-red-400")}
					title={detail}
				>
					{detail}
				</ItemDescription>
			</ItemContent>
			{repo.syncStatus !== "syncing" ? (
				<ItemActions>
					<Button
						variant="ghost"
						size="icon-sm"
						aria-label={m.syncRepository()}
						disabled={syncing}
						onClick={handleSync}
					>
						<RefreshCw className={cn(syncing && "animate-spin")} />
					</Button>
				</ItemActions>
			) : null}
		</Item>
	);
}
