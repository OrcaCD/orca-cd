import { AgentDataCards } from "@/components/cards/agents/data-cards";
import UpsertAgentDialog from "@/components/dialogs/upsert-agent";
import { type Agent } from "@/lib/agents";
import { useFetch } from "@/lib/api";
import { createFileRoute } from "@tanstack/react-router";
import { m } from "@/lib/paraglide/messages";

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
	const { data: agents, isLoading } = useFetch<Agent[]>("/agents");

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

			{agents && <AgentDataCards data={agents} />}
		</div>
	);
}
