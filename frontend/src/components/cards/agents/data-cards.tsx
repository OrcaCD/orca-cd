import { useMemo, useState } from "react";
import { AppWindow, EllipsisVertical, Search, Server, Trash2 } from "lucide-react";

import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import UpsertAgentDialog from "@/components/dialogs/upsert-agent";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuLabel,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { AgentStatus, deleteAgent, type Agent } from "@/lib/agents";
import { m } from "@/lib/paraglide/messages";
import { toSearchableText } from "@/lib/utils";
import { toast } from "sonner";

interface AgentDataCardsProps {
	data: Agent[];
}

interface AgentStatusBadgeProps {
	status: AgentStatus;
}

function AgentStatusBadge({ status }: AgentStatusBadgeProps) {
	const isOnline = status === AgentStatus.Online;
	const isError = status === AgentStatus.Error;
	const badgeVariant = isOnline ? "success" : isError ? "destructive" : "secondary";
	const statusLabel = isOnline ? m.statusOnline() : isError ? m.statusError() : m.statusOffline();
	const statusDotClass = isOnline ? "bg-emerald-500" : isError ? "bg-red-500" : "bg-zinc-400";

	return (
		<div className="flex items-center gap-2">
			<Badge variant={badgeVariant}>
				<span className={`h-2 w-2 rounded-full ${statusDotClass}`} aria-hidden="true" />
				{statusLabel}
			</Badge>
		</div>
	);
}

export function AgentDataCards({ data }: AgentDataCardsProps) {
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
			return data;
		}

		return data.filter((agent) => {
			const searchableAgent = {
				...agent,
				status: agent.status,
			};

			return toSearchableText(searchableAgent).includes(query);
		});
	}, [data, searchQuery]);

	return (
		<div className="space-y-4">
			<div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<div className="relative max-w-sm flex-1">
					<Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
					<Input
						value={searchQuery}
						onChange={(event) => setSearchQuery(event.target.value)}
						placeholder={m.searchAgents()}
						className="bg-muted pl-9"
					/>
				</div>

				<div className="text-sm text-muted-foreground">
					{m.totalAgentsCount({ count: data.length })}
				</div>
			</div>

			{filteredAgents.length === 0 ? (
				<div className="rounded-xl border border-dashed p-10 text-center">
					<p className="text-sm font-medium">{m.noAgentsFound()}</p>
					<p className="mt-1 text-sm text-muted-foreground">{m.noAgentsFoundDescription()}</p>
				</div>
			) : (
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
										<p className="mt-1 font-medium">{agent.appsCount ?? m.notAvailableShort()}</p>
									</div>
								</div>
							</CardContent>
						</Card>
					))}
				</div>
			)}
		</div>
	);
}
