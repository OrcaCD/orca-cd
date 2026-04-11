import fetcher, { API_BASE } from "./api";

export type RepositoryProvider =
	| "github"
	| "gitlab"
	| "generic"
	| "bitbucket"
	| "azure_devops"
	| "gitea";

export type RepositoryAuthMethod = "none" | "token" | "basic" | "ssh";
export type RepositorySyncType = "polling" | "webhook" | "manual";
export type RepositorySyncStatus = "unknown" | "syncing" | "failed" | "success";

export interface Repository {
	id: string;
	name: string;
	url: string;
	provider: RepositoryProvider;
	authMethod: RepositoryAuthMethod;
	syncType: RepositorySyncType;
	syncStatus: RepositorySyncStatus;
	lastSyncError: string | null;
	pollingIntervalSeconds: number | null;
	lastSyncedAt: string | null;
	createdBy: string;
	createdAt: string;
	updatedAt: string;
	apps: number;
}

export interface CreateRepositoryRequest {
	url: string;
	provider: RepositoryProvider;
	authMethod: RepositoryAuthMethod;
	authUser?: string;
	authToken?: string;
	syncType: RepositorySyncType;
	pollingIntervalSeconds?: number;
	webhookSecret?: string;
}

type UpdateRepositoryRequest = Omit<CreateRepositoryRequest, "provider">;

export interface TestConnectionRequest {
	url: string;
	provider: RepositoryProvider;
	authMethod: RepositoryAuthMethod;
	authUser?: string;
	authToken?: string;
}

export function listRepositories(): Promise<Repository[]> {
	return fetcher<Repository[]>(`${API_BASE}/repositories`);
}

export function createRepository(data: CreateRepositoryRequest): Promise<Repository> {
	return fetcher<Repository>(`${API_BASE}/repositories`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(data),
	});
}

export function testRepositoryConnection(data: TestConnectionRequest): Promise<void> {
	return fetcher(`${API_BASE}/repositories/test-connection`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(data),
	});
}

export function deleteRepository(id: string): Promise<void> {
	return fetcher(`${API_BASE}/repositories/${id}`, {
		method: "DELETE",
	});
}

export function updateRepository(id: string, data: UpdateRepositoryRequest): Promise<Repository> {
	return fetcher<Repository>(`${API_BASE}/repositories/${id}`, {
		method: "PUT",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(data),
	});
}
