import { columns } from "@/components/tables/repositories/columns";
import { RepositoryDataTable } from "@/components/tables/repositories/data-table";
import CreateRepositoryDialog from "@/components/dialogs/create-repository";
import { deleteRepository, syncRepository, type Repository } from "@/lib/repositories";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useFetch } from "@/lib/api";
import type { ApplicationListItem } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { useMemo, useState } from "react";
import { EllipsisVertical, RefreshCw, Search, Settings, Trash2 } from "lucide-react";

import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { Button } from "@/components/ui/button";
import { RepositoryCard } from "@/components/cards/repository-card";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/toast";
import { groupBy, toSearchableText } from "@/lib/utils";
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
	const { data: applications } = useFetch<ApplicationListItem[]>("/applications");
	const applicationsByRepository = useMemo(
		() => (applications ? groupBy(applications, (app) => app.repositoryId) : undefined),
		[applications],
	);

	const [searchQuery, setSearchQuery] = useState("");
	const { preferredLayout: viewMode, setPreferredLayout: setViewMode } = usePreferredLayout();

	async function handleDeleteRepo(repository: Repository) {
		try {
			await deleteRepository(repository.id);
			const repoIdentifier = repository?.name?.trim() || repository.id;
			toast.add({ type: "success", title: m.repositoryDeleted({ name: repoIdentifier }) });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedDeleteRepository(),
			});
		}
	}

	async function handleSyncRepo(repository: Repository) {
		try {
			await syncRepository(repository.id);
			toast.add({ type: "success", title: m.syncTriggered() });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedTriggerSync(),
			});
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
				<CreateRepositoryDialog />
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
								<RepositoryCard
									key={repository.id}
									repository={repository}
									apps={
										applicationsByRepository && (applicationsByRepository.get(repository.id) ?? [])
									}
									onSync={() => handleSyncRepo(repository)}
									action={
										<DropdownMenu>
											<DropdownMenuTrigger
												render={
													<Button variant="ghost" size="icon" className="h-8 w-8">
														<EllipsisVertical className="h-4 w-4" />
														<span className="sr-only">{m.cardActions()}</span>
													</Button>
												}
											></DropdownMenuTrigger>
											<DropdownMenuContent align="end" className="w-fit" keepMounted>
												<DropdownMenuGroup>
													<DropdownMenuLabel>{m.actions()}</DropdownMenuLabel>
													<DropdownMenuItem onClick={() => handleSyncRepo(repository)}>
														<RefreshCw className="h-4 w-4" />
														{m.sync()}
													</DropdownMenuItem>
													<DropdownMenuSeparator />
													<DropdownMenuItem
														render={
															<Link
																to="/repositories/$id/settings/auth"
																params={{ id: repository.id }}
															>
																<Settings className="h-4 w-4" />
																{m.settings()}
															</Link>
														}
													/>
													<DropdownMenuSeparator />
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
												</DropdownMenuGroup>
											</DropdownMenuContent>
										</DropdownMenu>
									}
								/>
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
