import { mutate } from "swr";
import { API_BASE, fetcher } from "./api";

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

export async function createAgent(data: CreateAgentRequest): Promise<AgentWithTokenResponse> {
	const res = await fetcher<AgentWithTokenResponse>("/agents", "POST", data);
	await mutate(`${API_BASE}/agents`);
	return res;
}

export async function updateAgent(id: string, data: UpdateAgentRequest): Promise<Agent> {
	const res = await fetcher<Agent>(`/agents/${id}`, "PUT", data);
	await mutate(`${API_BASE}/agents`);
	return res;
}

export async function deleteAgent(id: string): Promise<void> {
	await fetcher(`/agents/${id}`, "DELETE");
	await mutate(`${API_BASE}/agents`);
}
