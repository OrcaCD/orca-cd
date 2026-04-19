import { mutate } from "swr";
import { API_BASE, fetcher } from "./api";

export interface Application {
	id: string;
	name: string;
	syncStatus: SyncStatus;
	healthStatus: HealthStatus;
	repositoryId: string;
	repositoryName: string;
	branch: string;
	commit: string;
	commitMessage: string;
	lastSyncedAt?: string;
	path: string;
	agentId: string;
	agentName: string;
	createdAt: string;
	updatedAt: string;
}

export interface ApplicationListItem {
	id: string;
	name: string;
	syncStatus: SyncStatus;
	healthStatus: HealthStatus;
	repositoryName: string;
	agentName: string;
	branch: string;
	commit: string;
	lastSyncedAt?: string;
	path: string;
}

export enum SyncStatus {
	Synced = "synced",
	OutOfSync = "out_of_sync",
	Syncing = "syncing",
	Unknown = "unknown",
}

export enum HealthStatus {
	Healthy = "healthy",
	Unhealthy = "unhealthy",
	Unknown = "unknown",
}

export enum Type {
	Success = "success",
	Info = "info",
	Warning = "warning",
	Error = "error",
}

interface CreateApplicationRequest {
	name: string;
	repositoryId: string;
	agentId: string;
	branch: string;
	path: string;
}

interface UpdateApplicationRequest {
	name: string;
	repositoryId: string;
	agentId: string;
	branch: string;
	path: string;
}

export async function createApplication(data: CreateApplicationRequest): Promise<Application> {
	const res = await fetcher<Application>("/applications", "POST", data);
	await mutate(`${API_BASE}/applications`);
	return res;
}

export async function updateApplication(
	id: string,
	data: UpdateApplicationRequest,
): Promise<Application> {
	const res = await fetcher<Application>(`/applications/${id}`, "PUT", data);
	await mutate(`${API_BASE}/applications`);
	return res;
}

export async function deleteApplication(id: string): Promise<void> {
	await fetcher(`/applications/${id}`, "DELETE");
	await mutate(`${API_BASE}/applications`);
}
