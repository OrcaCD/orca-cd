import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { AgentVersionStatus, type Agent } from "@/lib/agents";
import { AlertTriangleIcon, OctagonAlertIcon } from "lucide-react";
import { m } from "@/lib/paraglide/messages";

export default function OutdatedAgentsAlert({ agents }: { agents: Agent[] }) {
	if (agents.some((agent) => agent.versionStatus === AgentVersionStatus.Incompatible)) {
		return (
			<Alert className="border-red-200 bg-red-50 text-red-900 dark:border-red-900 dark:bg-red-950 dark:text-red-50">
				<OctagonAlertIcon />
				<AlertTitle>{m.unsupportedAgentsAlertTitle()}</AlertTitle>
				<AlertDescription className="text-foreground text-wrap">
					{m.unsupportedAgentsAlertDescription()}
				</AlertDescription>
			</Alert>
		);
	}

	if (agents.some((agent) => agent.versionStatus === AgentVersionStatus.Outdated)) {
		return (
			<Alert className="border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-50">
				<AlertTriangleIcon />
				<AlertTitle>{m.outdatedAgentsAlertTitle()}</AlertTitle>
				<AlertDescription className="text-foreground text-wrap">
					{m.outdatedAgentsAlertDescription()}
				</AlertDescription>
			</Alert>
		);
	}

	return null;
}
