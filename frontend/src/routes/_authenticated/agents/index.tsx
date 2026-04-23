import UpsertAgentDialog from "@/components/dialogs/upsert-agent";
import { useFetch } from "@/lib/api";
import { createFileRoute } from "@tanstack/react-router";
import { m } from "@/lib/paraglide/messages";
import { useMemo, useState } from "react";
import {
	AppWindow,
	EllipsisVertical,
	LayoutGrid,
	List,
	Search,
	Server,
	Trash2,
} from "lucide-react";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuLabel,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { deleteAgent, type Agent } from "@/lib/agents";
import { toSearchableText } from "@/lib/utils";
import { toast } from "sonner";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { AgentsDataTable } from "@/components/tables/agents/data-table";
import { columns } from "@/components/tables/agents/columns";
import { AgentStatusBadge } from "@/components/badges/agent-status-badge";
import { toPreferredLayout, usePreferredLayout } from "@/lib/layout-preference";

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

	const { preferredLayout: viewMode, setPreferredLayout: setViewMode } = usePreferredLayout();
	const [searchQuery, setSearchQuery] = useState("");

	async function handleDeleteCard(agent: Agent) {
		try {
			await deleteAgent(agent.id);
			const agentIdentifier = agent?.name?.trim() || agent.id;
			toast.success(m.agentDeleted({ name: agentIdentifier }));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : m.failedDeleteAgent());
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
						<ToggleGroup
							type="single"
							variant="outline"
							value={viewMode}
							onValueChange={(value) => {
								const nextLayout = toPreferredLayout(value);
								if (nextLayout) {
									setViewMode(nextLayout);
								}
							}}
						>
							<ToggleGroupItem value="grid">
								<LayoutGrid className="h-4 w-4" />
							</ToggleGroupItem>

							<ToggleGroupItem value="table">
								<List className="h-4 w-4" />
							</ToggleGroupItem>
						</ToggleGroup>
					</div>
				</div>

				{viewMode === "grid" ? (
					<>
						{filteredAgents.length === 0 ? (
							<div className="rounded-xl border border-dashed p-10 text-center">
								<p className="text-sm font-medium">{m.noAgentsFound()}</p>
								<p className="mt-1 text-sm text-muted-foreground">{m.noAgentsFoundDescription()}</p>
							</div>
						) : null}
						<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
							{filteredAgents.map((agent) => (
								<Card key={agent.id} className="h-full border duration-300 hover:border-primary">
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
													<UpsertAgentDialog agent={agent} asDropdownItem />
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
												</DropdownMenuContent>
											</DropdownMenu>
										</CardAction>

										<div className="flex min-w-0 items-center gap-3">
											<div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-sm bg-muted/50">
												<Server className="h-5 w-5 text-muted-foreground" />
											</div>
											<div className="min-w-0 space-y-1">
												<CardTitle className="truncate" title={agent.name}>
													{agent.name}
												</CardTitle>
											</div>
										</div>
									</CardHeader>

									<hr className="mx-4" />

									<CardContent className="space-y-3">
										<AgentStatusBadge status={agent.status} />

										<div className="grid grid-cols-1 gap-2 text-xs">
											<div className="rounded-lg border bg-muted/50 p-2">
												<p className="flex items-center gap-1 text-muted-foreground">
													<AppWindow className="h-3 w-3" />
													{m.appsCount()}
												</p>
												<p className="mt-1 font-medium">
													{agent.appsCount ?? m.notAvailableShort()}
												</p>
											</div>
										</div>
									</CardContent>
								</Card>
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
