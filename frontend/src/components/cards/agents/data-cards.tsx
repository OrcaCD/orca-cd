import { useMemo, useState } from "react";
import { Cpu, EllipsisVertical, HardDrive, Search, Trash2 } from "lucide-react";

import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import UpsertAgentDialog from "@/components/dialogs/upsert-agent";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
	Card,
	CardAction,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { AgentStatus, deleteAgent, type Agent } from "@/lib/agents";
import { toSearchableText } from "@/lib/utils";
import { toast } from "sonner";

interface AgentDataCardsProps {
	data: Agent[];
}

function renderStatusBadge(status: AgentStatus) {
	const isOnline = status === AgentStatus.Online;

	return (
		<div className="flex items-center gap-2">
			<Badge variant={isOnline ? "success" : "secondary"}>
				<span
					className={`h-2 w-2 rounded-full ${isOnline ? "bg-emerald-500" : "bg-zinc-400"}`}
					aria-hidden="true"
				/>
				{isOnline ? "Online" : "Offline"}
			</Badge>
		</div>
	);
}

function metricValue(value?: number | string) {
	if (value === undefined || value === "") {
		return "n/a";
	}

	return String(`${value} %`);
}

export function AgentDataCards({ data }: AgentDataCardsProps) {
	const [searchQuery, setSearchQuery] = useState("");

	async function handleDeleteCard(agent: Agent) {
		try {
			await deleteAgent(agent.id);
			const agentIdentifier = agent?.name?.trim() || agent.id;
			toast.success(`Agent ${agentIdentifier} deleted successfully`);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : "Failed to delete agent");
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
				status: agent.status === AgentStatus.Online ? "online" : "offline",
			};

			return toSearchableText(searchableAgent).includes(query);
		});
	}, [searchQuery, data]);

	return (
		<div className="space-y-4">
			<div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<div className="text-sm text-muted-foreground">
					{filteredAgents.length} of {data.length} agents
				</div>

				<div className="relative w-full sm:max-w-md">
					<Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
					<Input
						value={searchQuery}
						onChange={(event) => setSearchQuery(event.target.value)}
						placeholder="Search agents by name, URL or IP..."
						className="bg-muted pl-9"
					/>
				</div>
			</div>

			{filteredAgents.length === 0 ? (
				<div className="rounded-xl border border-dashed p-10 text-center">
					<p className="text-sm font-medium">No agents found</p>
					<p className="mt-1 text-sm text-muted-foreground">
						Adjust your search to see matching agents.
					</p>
				</div>
			) : (
				<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
					{filteredAgents.map((agent) => (
						<Card key={agent.id} className="h-full">
							<CardHeader>
								<CardAction>
									<DropdownMenu>
										<DropdownMenuTrigger asChild>
											<Button variant="ghost" size="icon" className="h-8 w-8">
												<EllipsisVertical className="h-4 w-4" />
												<span className="sr-only">Card actions</span>
											</Button>
										</DropdownMenuTrigger>
										<DropdownMenuContent align="end">
											<UpsertAgentDialog agent={agent} asDropdownItem />
											<DropdownMenuSeparator />
											<ConfirmationDialog
												onConfirm={() => handleDeleteCard(agent)}
												title="Delete agent card?"
												description={`This will permanently delete "${agent.name}". This action cannot be undone.`}
												triggerText={
													<>
														<Trash2 className="h-4 w-4" />
														Delete
													</>
												}
												asDropdownItem
											/>
										</DropdownMenuContent>
									</DropdownMenu>
								</CardAction>

								<CardTitle>{agent.name}</CardTitle>
								<CardDescription>{agent.ip}</CardDescription>
							</CardHeader>

							<hr />

							<CardContent className="space-y-3">
								{renderStatusBadge(agent.status)}

								<div className="grid grid-cols-2 gap-2 text-xs">
									<div className="rounded-lg border bg-muted/50 p-2">
										<p className="flex items-center gap-1 text-muted-foreground">
											<Cpu className="h-3 w-3" />
											CPU Usage
										</p>
										<p className="mt-1 font-medium">{metricValue(agent.cpuUsage)}</p>
									</div>

									<div className="rounded-lg border bg-muted/50 p-2">
										<p className="flex items-center gap-1 text-muted-foreground">
											<HardDrive className="h-3 w-3" />
											Memory Usage
										</p>
										<p className="mt-1 font-medium">{metricValue(agent.memoryUsage)}</p>
									</div>
								</div>

								<div className="flex items-center gap-2 text-xs text-muted-foreground">
									<span>Docker {agent.dockerVersion ? agent.dockerVersion : "n/a"}</span>
								</div>
							</CardContent>
						</Card>
					))}
				</div>
			)}
		</div>
	);
}
