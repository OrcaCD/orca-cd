import {
	ApplicationEventStatus,
	ApplicationEventType,
	DeploymentStatus,
} from "@/lib/application-events";
import { Badge } from "../ui/badge";
import { m } from "@/lib/paraglide/messages";

const statusConfig: Record<
	ApplicationEventStatus,
	{ label: () => string; variant: "secondary" | "success" | "destructive" | "outline" }
> = {
	[ApplicationEventStatus.Running]: {
		label: () => m.eventStatusRunning(),
		variant: "secondary",
	},
	[ApplicationEventStatus.Succeeded]: {
		label: () => m.eventStatusSucceeded(),
		variant: "success",
	},
	[ApplicationEventStatus.Failed]: {
		label: () => m.eventStatusFailed(),
		variant: "destructive",
	},
	[ApplicationEventStatus.NoChange]: {
		label: () => m.eventStatusNoChange(),
		variant: "outline",
	},
};

export function ApplicationEventStatusBadge({
	status,
	type,
	deploymentStatus,
}: {
	status: ApplicationEventStatus;
	type?: ApplicationEventType;
	deploymentStatus?: DeploymentStatus | null;
}) {
	if (type === ApplicationEventType.Rollback && status === ApplicationEventStatus.Succeeded) {
		return (
			<Badge variant="outline" className="border-amber-500/40 bg-amber-500/10 text-amber-500">
				{m.eventStatusRolledBack()}
			</Badge>
		);
	}
	if (
		status === ApplicationEventStatus.Running &&
		deploymentStatus === DeploymentStatus.Verifying
	) {
		return <Badge variant="secondary">{m.eventStatusVerifying()}</Badge>;
	}

	const config = statusConfig[status];
	if (!config) {
		return <Badge variant="outline">{status}</Badge>;
	}

	return <Badge variant={config.variant}>{config.label()}</Badge>;
}
