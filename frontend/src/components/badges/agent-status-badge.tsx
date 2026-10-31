import { AgentStatus } from "@/lib/agents";
import { Badge } from "../ui/badge";
import { InfoTooltip, InfoTooltipContent, InfoTooltipTrigger } from "@/components/info-tooltip";
import { m } from "@/lib/paraglide/messages";

export function AgentStatusBadge({ status }: { status: AgentStatus }) {
	const isOnline = status === AgentStatus.Online;
	const isError = status === AgentStatus.Error;
	const badgeVariant = isOnline ? "success" : isError ? "destructive" : "secondary";
	const statusLabel = isOnline ? m.statusOnline() : isError ? m.statusError() : m.statusOffline();
	const statusDotClass = isOnline ? "bg-emerald-500" : isError ? "bg-red-500" : "bg-zinc-400";
	const statusDot = (
		<span className={`h-2 w-2 rounded-full ${statusDotClass}`} aria-hidden="true" />
	);

	return (
		<div className="flex items-center gap-2">
			{isError ? (
				<InfoTooltip>
					<InfoTooltipTrigger
						render={<Badge tabIndex={0} variant={badgeVariant} className="cursor-help" />}
					>
						{statusDot}
						{statusLabel}
					</InfoTooltipTrigger>
					<InfoTooltipContent>{m.agentStatusErrorDescription()}</InfoTooltipContent>
				</InfoTooltip>
			) : (
				<Badge variant={badgeVariant}>
					{statusDot}
					{statusLabel}
				</Badge>
			)}
		</div>
	);
}
