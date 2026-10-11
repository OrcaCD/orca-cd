import { AgentVersionStatus } from "@/lib/agents";
import { cn } from "@/lib/utils";
import { AlertTriangle, CircleHelp, OctagonAlert } from "lucide-react";
import { Badge } from "../ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";
import { m } from "@/lib/paraglide/messages";

const statusConfig: Record<
	Exclude<AgentVersionStatus, AgentVersionStatus.UpToDate>,
	{ label: () => string; description: () => string; className: string; Icon: typeof AlertTriangle }
> = {
	[AgentVersionStatus.Outdated]: {
		label: () => m.agentVersionOutdated(),
		description: () => m.agentVersionOutdatedDescription(),
		className: "bg-amber-500/20 text-amber-600 dark:text-amber-400 border-amber-500/30",
		Icon: AlertTriangle,
	},
	[AgentVersionStatus.Incompatible]: {
		label: () => m.agentVersionUnsupported(),
		description: () => m.agentVersionUnsupportedDescription(),
		className: "bg-red-500/20 text-red-600 dark:text-red-400 border-red-500/30",
		Icon: OctagonAlert,
	},
	[AgentVersionStatus.Unknown]: {
		label: () => m.unknown(),
		description: () => m.agentVersionUnknownDescription(),
		className: "bg-zinc-500/20 text-zinc-600 dark:text-zinc-400 border-zinc-500/30",
		Icon: CircleHelp,
	},
};

export function AgentVersionBadge({
	version,
	status,
}: {
	version?: string | null;
	status: AgentVersionStatus;
}) {
	// Non-release builds (e.g. "dev") report a version that cannot be compared.
	if (
		status === AgentVersionStatus.UpToDate ||
		(status === AgentVersionStatus.Unknown && version)
	) {
		return <Badge variant="outline">{version}</Badge>;
	}

	const { label, description, className, Icon } = statusConfig[status];
	const text = version ? `${version} · ${label()}` : label();

	return (
		<Tooltip>
			<TooltipTrigger render={<Badge tabIndex={0} className={cn("cursor-help", className)} />}>
				<Icon aria-hidden="true" />
				{text}
			</TooltipTrigger>
			<TooltipContent>{description()}</TooltipContent>
		</Tooltip>
	);
}
