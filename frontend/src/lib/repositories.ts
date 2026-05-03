import { fetcher } from "./api";

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
	appCount: number;
	webhookSecret?: string | undefined;
	webhookUrl?: string | undefined;
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

export interface UpdateRepositoryRequest {
	authMethod?: RepositoryAuthMethod;
	authUser?: string;
	authToken?: string;
	syncType?: RepositorySyncType;
	pollingIntervalSeconds?: number;
	webhookSecret?: string;
}

export interface TestConnectionRequest {
	url: string;
	provider: RepositoryProvider;
	authMethod: RepositoryAuthMethod;
	authUser?: string;
	authToken?: string;
}

export function createRepository(data: CreateRepositoryRequest): Promise<Repository> {
	return fetcher<Repository>("/repositories", "POST", data);
}

export function testRepositoryConnection(data: TestConnectionRequest): Promise<void> {
	return fetcher("/repositories/test-connection", "POST", data);
}

export function deleteRepository(id: string): Promise<void> {
	return fetcher(`/repositories/${id}`, "DELETE");
}

export function updateRepository(id: string, data: UpdateRepositoryRequest): Promise<Repository> {
	return fetcher<Repository>(`/repositories/${id}`, "PUT", data);
}

export function syncRepository(id: string): Promise<void> {
	return fetcher(`/repositories/${id}/sync`, "POST");
}

export function getGitProviderIconPath(provider: RepositoryProvider): string {
	const iconName = provider === "generic" ? "git" : provider;

	return `/assets/icons/${iconName}.svg`;
}

export function getGitProviderIconClass(provider: RepositoryProvider): string {
	return provider === "github" ? "dark:invert" : "";
}
