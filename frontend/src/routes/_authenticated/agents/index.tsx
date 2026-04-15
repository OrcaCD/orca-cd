import { AgentDataCards } from "@/components/cards/agents/data-cards";
import UpsertAgentDialog from "@/components/dialogs/upsert-agent";
import { type Agent } from "@/lib/agents";
import { useFetch } from "@/lib/api";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/agents/")({
	component: RouteComponent,
	head: () => ({
		meta: [
			{
				title: "Agents",
			},
		],
	}),
});

function RouteComponent() {
	const { data: agents, isLoading } = useFetch<Agent[]>("/agents");

	return (
		<div className="p-6 space-y-6">
			<div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
				<div>
					<h1 className="text-2xl font-bold">Agents</h1>
					<p className="text-muted-foreground text-sm mt-1">Manage Docker hosts and servers</p>
				</div>
				<UpsertAgentDialog agent={null} />
			</div>

			{isLoading && <p className="text-muted-foreground text-sm">Loading agents...</p>}

			{agents && <AgentDataCards data={agents} />}
		</div>
	);
}
