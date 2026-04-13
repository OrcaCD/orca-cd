import { fetcher } from "./api";

export enum AgentStatus {
	Online = 0,
	Offline = 1,
}

export interface Agent {
	id: string;
	name: string;
	ip: string;
	status: AgentStatus;
	cpuUsage?: number;
	memoryUsage?: number;
	appsCount?: number;
	dockerVersion?: string;
}

export interface CreateAgentRequest {
	name: string;
	ip: string;
}

export interface UpdateAgentRequest {
	name: string;
	ip: string;
}

export function createAgent(data: CreateAgentRequest): Promise<Agent> {
	return fetcher<Agent>("/agents", "POST", data);
}

export function updateAgent(id: string, data: UpdateAgentRequest): Promise<Agent> {
	return fetcher<Agent>(`/agents/${id}`, "PUT", data);
}

export function deleteAgent(id: string): Promise<void> {
	return fetcher(`/agents/${id}`, "DELETE");
}
