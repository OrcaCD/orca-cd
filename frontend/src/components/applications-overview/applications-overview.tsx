import type { Agent } from "@/lib/agents";
import type { ApplicationListItem } from "@/lib/applications";
import { AgentsStatusCard } from "./agents-status-card";
import { AttentionList } from "./attention-list";
import { DeploymentActivityCard } from "./deployment-activity-card";
import { getAttentionItems, type FleetCategory } from "./fleet";
import { FleetStatusCard } from "./fleet-status-card";

interface ApplicationsOverviewProps {
	apps: ApplicationListItem[];
	agents: Agent[] | undefined;
	filter: FleetCategory | null;
	onFilterChange: (filter: FleetCategory | null) => void;
}

export function ApplicationsOverview({
	apps,
	agents,
	filter,
	onFilterChange,
}: ApplicationsOverviewProps) {
	return (
		<div className="space-y-4">
			<div className="grid gap-4 md:grid-cols-2 xl:grid-cols-[3fr_2fr_2fr]">
				<FleetStatusCard apps={apps} filter={filter} onFilterChange={onFilterChange} />
				<DeploymentActivityCard />
				<AgentsStatusCard agents={agents} />
			</div>
			<AttentionList items={getAttentionItems(apps, agents)} />
		</div>
	);
}
