import { columns } from "@/components/tables/repositories/columns";
import { RepositoryDataTable } from "@/components/tables/repositories/data-table";
import UpsertRepositoryDialog from "@/components/dialogs/upsert-repository";
import {
	deleteRepository,
	syncRepository,
	getGitProviderIconClass,
	getGitProviderIconPath,
	type Repository,
} from "@/lib/repositories";
import { createFileRoute } from "@tanstack/react-router";
import { useFetch } from "@/lib/api";
import { m } from "@/lib/paraglide/messages";
import { useMemo, useState } from "react";
import { AppWindow, EllipsisVertical, RefreshCw, Search, Trash2 } from "lucide-react";

import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { toast } from "sonner";
import { toSearchableText } from "@/lib/utils";
import { RepositoryStatusBadge } from "@/components/badges/repository-status-badge";
import { RepositorySyncTypeBadge } from "@/components/badges/repository-sync-type-badge";
import { usePreferredLayout } from "@/lib/layout-preference";
import { LayoutToggleGroup } from "@/components/layout-toggle-group";

export const Route = createFileRoute("/_authenticated/repositories/")({
	component: RepositoriesPage,
	head: () => ({
		meta: [
			{
				title: m.pageRepositories(),
			},
		],
	}),
});

function RepositoriesPage() {
	const { data, isLoading } = useFetch<Repository[]>("/repositories");

	const [searchQuery, setSearchQuery] = useState("");
	const { preferredLayout: viewMode, setPreferredLayout: setViewMode } = usePreferredLayout();

	async function handleDeleteRepo(repository: Repository) {
		try {
			await deleteRepository(repository.id);
			const repoIdentifier = repository?.name?.trim() || repository.id;
			toast.success(m.repositoryDeleted({ name: repoIdentifier }));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : m.failedDeleteRepository());
		}
	}

	async function handleSyncRepo(repository: Repository) {
		try {
			await syncRepository(repository.id);
			toast.success(m.syncTriggered());
		} catch (err) {
			toast.error(err instanceof Error ? err.message : m.failedTriggerSync());
		}
	}

	const filteredRepositories = useMemo(() => {
		const query = searchQuery.trim().toLowerCase();

		if (!query) {
			return data ?? [];
		}

		return (
			data?.filter((repository) => {
				return toSearchableText(repository).includes(query);
			}) ?? []
		);
	}, [data, searchQuery]);

	return (
		<div className="p-6 space-y-6">
			<div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
				<div>
					<h1 className="text-2xl font-bold">{m.pageRepositories()}</h1>
					<p className="text-muted-foreground text-sm mt-1">{m.repositoriesPageDescription()}</p>
				</div>
				<UpsertRepositoryDialog />
			</div>

			{isLoading && <p className="text-muted-foreground text-sm">{m.loadingRepositories()}</p>}

			<div className="space-y-4">
				<div className="pb-2 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
					<div className="relative flex-1 ">
						<Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
						<Input
							value={searchQuery}
							onChange={(event) => setSearchQuery(event.target.value)}
							placeholder={m.searchRepositories()}
							className="pl-9 bg-muted border-border"
						/>
					</div>

					<div className="flex gap-2 ">
						<LayoutToggleGroup viewMode={viewMode} setViewMode={setViewMode} />
					</div>
				</div>

				{viewMode === "grid" ? (
					<>
						{filteredRepositories.length === 0 && !isLoading ? (
							<div className="rounded-xl border border-dashed p-10 text-center">
								<p className="text-sm font-medium">{m.noRepositoriesFound()}</p>
								<p className="mt-1 text-sm text-muted-foreground">
									{m.noRepositoriesFoundDescription()}
								</p>
							</div>
						) : null}
						<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
							{filteredRepositories.map((repository) => (
								<Card
									key={repository.id}
									className="h-full border duration-300 hover:border-primary"
								>
									<CardHeader>
										<CardAction>
											<DropdownMenu>
												<DropdownMenuTrigger asChild>
													<Button variant="ghost" size="icon" className="h-8 w-8">
														<EllipsisVertical className="h-4 w-4" />
														<span className="sr-only">{m.cardActions()}</span>
													</Button>
												</DropdownMenuTrigger>
												<DropdownMenuContent align="end">
													<DropdownMenuLabel>{m.actions()}</DropdownMenuLabel>
													<DropdownMenuItem onClick={() => handleSyncRepo(repository)}>
														<RefreshCw className="h-4 w-4" />
														{m.sync()}
													</DropdownMenuItem>
													<UpsertRepositoryDialog existingRepository={repository} asDropdownItem />
													<ConfirmationDialog
														onConfirm={() => handleDeleteRepo(repository)}
														title={m.deleteRepositoryTitle()}
														description={m.deleteRepositoryDescription({ name: repository.name })}
														triggerText={
															<>
																<Trash2 className="h-4 w-4" />
																{m.delete()}
															</>
														}
														asDropdownItem
													/>
												</DropdownMenuContent>
											</DropdownMenu>
										</CardAction>

										<div className="flex min-w-0 items-center gap-3">
											<div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-sm bg-muted/50">
												<img
													src={getGitProviderIconPath(repository.provider)}
													alt={m.gitProviderAlt()}
													className={`h-7 w-7 ${getGitProviderIconClass(repository.provider)}`}
												/>
											</div>
											<div className="min-w-0 space-y-1">
												<CardTitle className="truncate" title={repository.name}>
													{repository.name}
												</CardTitle>
											</div>
										</div>
									</CardHeader>

									<hr className="mx-4" />

									<CardContent className="space-y-3">
										<RepositoryStatusBadge status={repository.syncStatus} />

										<div className="grid grid-cols-1 gap-2 text-xs">
											<div className="rounded-lg border bg-muted/50 p-2">
												<p className="flex items-center gap-1 text-muted-foreground">
													<AppWindow className="h-3 w-3" />
													{m.appsCount()}
												</p>
												<p className="mt-1 font-medium">
													{repository.appCount ?? m.notAvailableShort()}
												</p>
											</div>
										</div>

										<div className="flex justify-between">
											<RepositorySyncTypeBadge syncType={repository.syncType} />

											<span className="text-muted-foreground">
												{repository.lastSyncedAt
													? new Date(repository.lastSyncedAt).toLocaleString()
													: m.never()}
											</span>
										</div>
									</CardContent>
								</Card>
							))}
						</div>
					</>
				) : (
					<RepositoryDataTable columns={columns} data={filteredRepositories} />
				)}
			</div>
		</div>
	);
}
