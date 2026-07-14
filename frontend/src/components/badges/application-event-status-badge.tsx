import { ApplicationEventStatus } from "@/lib/application-events";
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

export function ApplicationEventStatusBadge({ status }: { status: ApplicationEventStatus }) {
	const config = statusConfig[status];
	if (!config) {
		return <Badge variant="outline">{status}</Badge>;
	}

	return <Badge variant={config.variant}>{config.label()}</Badge>;
}
