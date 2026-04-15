import { mutate } from "swr";
import { API_BASE, fetcher } from "./api";

export enum AgentStatus {
	Online = 0,
	Offline = 1,
}

export interface Agent {
	id: string;
	name: string;
	ip: string;
	status: AgentStatus;
	appsCount?: number;
	dockerVersion?: string;
}

export interface CreateAgentRequest {
	name: string;
}

export interface UpdateAgentRequest {
	name: string;
}

export async function createAgent(data: CreateAgentRequest): Promise<Agent> {
	const res = await fetcher<Agent>("/agents", "POST", data);
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
