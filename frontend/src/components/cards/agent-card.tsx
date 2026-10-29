import type { ReactNode } from "react";
import { AgentVersionBadge } from "@/components/badges/agent-version-badge";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import { AgentStatus, type Agent } from "@/lib/agents";
import { m } from "@/lib/paraglide/messages";
import { ResourceCard, type FlowStage } from "./resource-card";

// An agent in the error state is connected to the hub, but cannot reach the Docker daemon.
function getDockerStage(agent: Agent): FlowStage {
	const label = m.cardStageDocker();
	switch (agent.status) {
		case AgentStatus.Online:
			return {
				label,
				value: agent.dockerVersion ?? m.notAvailableShort(),
				state: "ok",
				mono: true,
			};
		case AgentStatus.Error:
			return {
				label,
				value: m.cardDockerUnreachable(),
				title: m.agentStatusErrorDescription(),
				state: "error",
			};
		default:
			return { label, value: agent.dockerVersion ?? m.unknown(), state: "unknown", mono: true };
	}
}

export function AgentCard({ agent, action }: { agent: Agent; action?: ReactNode }) {
	const connected = agent.status !== AgentStatus.Offline;

	return (
		<ResourceCard
			icon={<StaticLucideIcon name={agent.icon} className="size-5 text-primary" />}
			title={agent.name}
			action={action}
			stages={[
				{
					label: m.cardStageHub(),
					value: connected ? m.statusOnline() : m.statusOffline(),
					state: connected ? "ok" : "error",
				},
				getDockerStage(agent),
				{
					label: m.cardStageApplications(),
					value: agent.appsCount ?? m.notAvailableShort(),
					state: agent.appsCount ? "info" : "off",
				},
			]}
			meta={<AgentVersionBadge version={agent.version} status={agent.versionStatus} />}
			time={agent.lastSeen ?? null}
		/>
	);
}
