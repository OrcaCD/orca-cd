import { fetcher } from "./api";

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

export function createApplication(data: CreateApplicationRequest): Promise<Application> {
	return fetcher<Application>("/applications", "POST", data);
}

export function updateApplication(
	id: string,
	data: UpdateApplicationRequest,
): Promise<Application> {
	return fetcher<Application>(`/applications/${id}`, "PUT", data);
}

export function deleteApplication(id: string): Promise<void> {
	return fetcher(`/applications/${id}`, "DELETE");
}
