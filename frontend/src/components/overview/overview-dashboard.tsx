import type { Agent } from "@/lib/agents";
import type { ApplicationListItem } from "@/lib/applications";
import { AgentsStatusCard } from "./agents-status-card";
import { AttentionList } from "./attention-list";
import { DeploymentActivityCard } from "./deployment-activity-card";
import { getAttentionItems } from "@/lib/fleet";
import { FleetStatusCard } from "./fleet-status-card";
import { RecentDeploymentsCard } from "./recent-deployments-card";
import { RepositoriesCard } from "./repositories-card";

interface OverviewDashboardProps {
	apps: ApplicationListItem[];
	agents: Agent[] | undefined;
}

export function OverviewDashboard({ apps, agents }: OverviewDashboardProps) {
	return (
		<div className="space-y-4">
			<div className="grid gap-4 md:grid-cols-2 xl:grid-cols-[3fr_2fr_2fr]">
				<FleetStatusCard apps={apps} />
				<DeploymentActivityCard />
				<AgentsStatusCard agents={agents} />
			</div>
			<AttentionList items={getAttentionItems(apps, agents)} />
			<div className="grid gap-4 lg:grid-cols-[3fr_2fr]">
				<RecentDeploymentsCard />
				<RepositoriesCard />
			</div>
		</div>
	);
}
