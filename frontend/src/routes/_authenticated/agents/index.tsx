import { AgentDataCards } from "@/components/cards/agents/data-cards";
import UpsertAgentDialog from "@/components/dialogs/upsert-agent";
import { AgentStatus, type Agent } from "@/lib/agents";
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

const mockAgents: Agent[] = [
	{
		id: "1",
		name: "prod-server-01",
		ip: "192.168.1.10",
		url: "https://prod-server-01.example.com",
		status: AgentStatus.Online,
		cpuUsage: 22,
		memoryUsage: 61,
		dockerVersion: "28.2.2",
	},
	{
		id: "2",
		name: "staging-server-02",
		ip: "192.168.1.22",
		url: "https://staging-server-02.example.com",
		status: AgentStatus.Offline,
		dockerVersion: "27.5.1",
	},
	{
		id: "3",
		name: "edge-node-eu-01",
		ip: "10.0.20.8",
		url: "https://edge-eu-01.example.com",
		status: AgentStatus.Online,
		cpuUsage: 37,
		memoryUsage: 48,
		dockerVersion: "28.1.0",
	},
	{
		id: "4",
		name: "backup-host-01",
		ip: "172.18.0.3",
		url: "https://backup-host-01.example.com",
		status: AgentStatus.Online,
		cpuUsage: 11,
		memoryUsage: 34,
		dockerVersion: "27.3.1",
	},
];

function RouteComponent() {
	return (
		<div className="p-6 space-y-6">
			<div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
				<div>
					<h1 className="text-2xl font-bold">Agents</h1>
					<p className="text-muted-foreground text-sm mt-1">Manage Docker hosts and servers</p>
				</div>
				<UpsertAgentDialog agent={null} />
			</div>

			<AgentDataCards data={mockAgents} />
		</div>
	);
}
