import UpsertAgentDialog from "@/components/dialogs/upsert-agent";
import { PageContainer, PageHeader } from "@/components/page-header";
import {
	EmptyState,
	ResourceGridSkeleton,
	ResourceTableSkeleton,
} from "@/components/collection-states";
import { SearchField } from "@/components/search-field";
import { useFetch } from "@/lib/api";
import { createFileRoute } from "@tanstack/react-router";
import { m } from "@/lib/paraglide/messages";
import { useMemo, useRef, useState } from "react";
import { EllipsisVertical, Server, Trash2 } from "lucide-react";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { Button } from "@/components/ui/button";
import { AgentCard } from "@/components/cards/agent-card";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { deleteAgent, type Agent } from "@/lib/agents";
import type { ApplicationListItem } from "@/lib/applications";
import { groupBy, toSearchableText } from "@/lib/utils";
import { toast } from "@/components/ui/toast";
import { AgentsDataTable } from "@/components/tables/agents/data-table";
import { columns } from "@/components/tables/agents/columns";
import OutdatedAgentsAlert from "@/components/alerts/outdated-agents-alert";
import { usePreferredLayout } from "@/lib/layout-preference";
import { LayoutToggleGroup } from "@/components/layout-toggle-group";
import RotateAgentTokenDialog from "@/components/dialogs/rotate-agent-token";

export const Route = createFileRoute("/_authenticated/agents/")({
	component: RouteComponent,
	head: () => ({
		meta: [
			{
				title: m.pageAgents(),
			},
		],
	}),
});

function RouteComponent() {
	const { data, isLoading } = useFetch<Agent[]>("/agents");
	const { data: applications } = useFetch<ApplicationListItem[]>("/applications");
	const applicationsByAgent = useMemo(
		() => (applications ? groupBy(applications, (app) => app.agentId) : undefined),
		[applications],
	);

	const { preferredLayout: viewMode, setPreferredLayout: setViewMode } = usePreferredLayout();
	const [searchQuery, setSearchQuery] = useState("");
	const searchInputRef = useRef<HTMLInputElement>(null);

	async function handleDeleteCard(agent: Agent) {
		try {
			await deleteAgent(agent.id);
			const agentIdentifier = agent?.name?.trim() || agent.id;
			toast.add({ type: "success", title: m.agentDeleted({ name: agentIdentifier }) });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedDeleteAgent(),
			});
		}
	}

	const filteredAgents = useMemo(() => {
		const query = searchQuery.trim().toLowerCase();

		if (!query) {
			return data ?? [];
		}

		return (
			data?.filter((agent) => {
				const searchableAgent = {
					...agent,
					status: agent.status,
				};

				return toSearchableText(searchableAgent).includes(query);
			}) ?? []
		);
	}, [data, searchQuery]);

	return (
		<PageContainer>
			<PageHeader
				title={m.pageAgents()}
				description={m.agentsPageDescription()}
				actions={<UpsertAgentDialog agent={null} />}
			/>

			<OutdatedAgentsAlert agents={data ?? []} />

			<div className="space-y-4">
				{(data?.length !== 0 || searchQuery.length > 0) && (
					<div className="flex items-center gap-2 sm:gap-3">
						<SearchField
							value={searchQuery}
							onChange={setSearchQuery}
							inputRef={searchInputRef}
							label={m.searchAgentsLabel()}
							placeholder={m.searchAgents()}
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
							kind="agent"
							label={m.loadingAgents()}
							className="sm:grid-cols-2 xl:grid-cols-3"
						/>
					) : (
						<ResourceTableSkeleton label={m.loadingAgents()} />
					)
				) : data?.length === 0 ? (
					<EmptyState
						icon={Server}
						title={m.noAgentsYet()}
						description={m.noAgentsYetDescription()}
						action={<UpsertAgentDialog agent={null} />}
					/>
				) : filteredAgents.length === 0 ? (
					<EmptyState
						variant="noMatches"
						title={m.noAgentsFound()}
						description={m.noAgentsFoundDescription()}
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
							{filteredAgents.map((agent) => (
								<AgentCard
									key={agent.id}
									agent={agent}
									apps={applicationsByAgent && (applicationsByAgent.get(agent.id) ?? [])}
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
													<UpsertAgentDialog agent={agent} asDropdownItem />
													<RotateAgentTokenDialog agent={agent} />
													<DropdownMenuSeparator />
													<ConfirmationDialog
														onConfirm={() => handleDeleteCard(agent)}
														title={m.deleteAgentCardTitle()}
														description={m.deleteAgentCardDescription({ name: agent.name })}
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
					<AgentsDataTable columns={columns} data={filteredAgents} />
				)}
			</div>
		</PageContainer>
	);
}
