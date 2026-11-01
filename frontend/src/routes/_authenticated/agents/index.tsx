import UpsertAgentDialog from "@/components/dialogs/upsert-agent";
import { useFetch } from "@/lib/api";
import { createFileRoute } from "@tanstack/react-router";
import { m } from "@/lib/paraglide/messages";
import { useMemo, useState } from "react";
import { EllipsisVertical, Search, Trash2 } from "lucide-react";
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
import { Input } from "@/components/ui/input";
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
		<div className="p-6 space-y-6">
			<div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
				<div>
					<h1 className="text-2xl font-bold">{m.pageAgents()}</h1>
					<p className="text-muted-foreground text-sm mt-1">{m.agentsPageDescription()}</p>
				</div>
				<UpsertAgentDialog agent={null} />
			</div>

			{isLoading && <p className="text-muted-foreground text-sm">{m.loadingAgents()}</p>}

			<OutdatedAgentsAlert agents={data ?? []} />

			<div className="space-y-4">
				<div className="pb-2 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
					<div className="relative flex-1 ">
						<Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
						<Input
							value={searchQuery}
							onChange={(event) => setSearchQuery(event.target.value)}
							placeholder={m.searchAgents()}
							className="pl-9 bg-muted border-border"
						/>
					</div>

					<div className="flex gap-2 ">
						<LayoutToggleGroup viewMode={viewMode} setViewMode={setViewMode} />
					</div>
				</div>

				{viewMode === "grid" ? (
					<>
						{filteredAgents.length === 0 && !isLoading ? (
							<div className="rounded-xl border border-dashed p-10 text-center">
								<p className="text-sm font-medium">{m.noAgentsFound()}</p>
								<p className="mt-1 text-sm text-muted-foreground">{m.noAgentsFoundDescription()}</p>
							</div>
						) : null}
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
		</div>
	);
}
