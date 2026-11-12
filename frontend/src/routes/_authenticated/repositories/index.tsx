import { columns } from "@/components/tables/repositories/columns";
import { PageContainer, PageHeader } from "@/components/page-header";
import {
	EmptyState,
	ResourceGridSkeleton,
	ResourceTableSkeleton,
} from "@/components/collection-states";
import { SearchField } from "@/components/search-field";
import { RepositoryDataTable } from "@/components/tables/repositories/data-table";
import CreateRepositoryDialog from "@/components/dialogs/create-repository";
import { useSharedDialog } from "@/hooks/use-shared-dialog";
import { deleteRepository, syncRepository, type Repository } from "@/lib/repositories";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useFetch } from "@/lib/api";
import type { ApplicationListItem } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { useCanOnApplicationsOf, usePermissions } from "@/lib/permissions";
import { useMemo, useRef, useState } from "react";
import { EllipsisVertical, GitBranch, Plus, RefreshCw, Settings, Trash2 } from "lucide-react";

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
	const searchInputRef = useRef<HTMLInputElement>(null);
	// The empty state disappears as soon as the first repository exists, its webhook summary has to stay.
	const {
		fallbackRef: addRepositoryRef,
		triggerProps: addRepositoryTrigger,
		openFrom: openAddRepository,
		dialogProps: addRepositoryDialog,
	} = useSharedDialog();
	const { can } = usePermissions();
	const canOnApplicationsOf = useCanOnApplicationsOf("repositoryId");
	const canCreate = can("repository", "create");
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
		<PageContainer>
			<PageHeader
				title={m.pageRepositories()}
				description={m.repositoriesPageDescription()}
				actions={
					canCreate && (
						<Button {...addRepositoryTrigger} ref={addRepositoryRef} onClick={openAddRepository}>
							<Plus />
							{m.addRepository()}
						</Button>
					)
				}
			/>
			{canCreate && <CreateRepositoryDialog {...addRepositoryDialog} />}

			<div className="space-y-4">
				{(data?.length !== 0 || searchQuery.length > 0) && (
					<div className="flex items-center gap-2 sm:gap-3">
						<SearchField
							value={searchQuery}
							onChange={setSearchQuery}
							inputRef={searchInputRef}
							label={m.searchRepositoriesLabel()}
							placeholder={m.searchRepositories()}
							className="flex-1"
						/>

						<div className="flex shrink-0 gap-2">
							<LayoutToggleGroup viewMode={viewMode} setViewMode={setViewMode} />
						</div>
					</div>
				)}

				{isLoading ? (
					viewMode === "grid" ? (
						<ResourceGridSkeleton
							kind="repository"
							label={m.loadingRepositories()}
							className="sm:grid-cols-2 xl:grid-cols-3"
						/>
					) : (
						<ResourceTableSkeleton label={m.loadingRepositories()} />
					)
				) : data?.length === 0 ? (
					<EmptyState
						icon={GitBranch}
						title={m.noRepositoriesYet()}
						description={m.noRepositoriesYetDescription()}
						action={
							canCreate && (
								<Button {...addRepositoryTrigger} onClick={openAddRepository}>
									<Plus />
									{m.addRepository()}
								</Button>
							)
						}
					/>
				) : filteredRepositories.length === 0 ? (
					<EmptyState
						variant="noMatches"
						title={m.noRepositoriesFound()}
						description={m.noRepositoriesFoundDescription()}
						action={
							<Button
								variant="outline"
								onClick={() => {
									setSearchQuery("");
									searchInputRef.current?.focus();
								}}
							>
								{m.searchClear()}
							</Button>
						}
					/>
				) : viewMode === "grid" ? (
					<>
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
													{can("repository", "sync", repository.id) &&
														canOnApplicationsOf(repository.id, "deploy", repository.appCount) && (
															<>
																<DropdownMenuItem onClick={() => handleSyncRepo(repository)}>
																	<RefreshCw className="h-4 w-4" />
																	{m.sync()}
																</DropdownMenuItem>
																<DropdownMenuSeparator />
															</>
														)}
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
													{can("repository", "delete", repository.id) &&
														canOnApplicationsOf(repository.id, "delete", repository.appCount) && (
															<>
																<DropdownMenuSeparator />
																<ConfirmationDialog
																	onConfirm={() => handleDeleteRepo(repository)}
																	title={m.deleteRepositoryTitle()}
																	description={m.deleteRepositoryDescription({
																		name: repository.name,
																	})}
																	triggerText={
																		<>
																			<Trash2 className="h-4 w-4" />
																			{m.delete()}
																		</>
																	}
																	asDropdownItem
																/>
															</>
														)}
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
		</PageContainer>
	);
}
