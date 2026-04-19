import { fetcher } from "./api";

export enum AgentStatus {
	Offline = "offline",
	Online = "online",
	Error = "error",
}

export interface Agent {
	id: string;
	name: string;
	status: AgentStatus;
	appsCount?: number;
	lastSeen?: string;
	createdAt: string;
	updatedAt: string;
}

export interface AgentWithTokenResponse extends Agent {
	authToken: string;
}

export interface CreateAgentRequest {
	name: string;
}

export interface UpdateAgentRequest {
	name: string;
}

export function createAgent(data: CreateAgentRequest): Promise<AgentWithTokenResponse> {
	return fetcher<AgentWithTokenResponse>("/agents", "POST", data);
}

export function updateAgent(id: string, data: UpdateAgentRequest): Promise<Agent> {
	return fetcher<Agent>(`/agents/${id}`, "PUT", data);
}

export function deleteAgent(id: string): Promise<void> {
	return fetcher(`/agents/${id}`, "DELETE");
}
