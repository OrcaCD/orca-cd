import { mutate } from "swr";
import { API_BASE, fetcher } from "./api";

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

type UpdateRepositoryRequest = Omit<CreateRepositoryRequest, "provider">;

export interface TestConnectionRequest {
	url: string;
	provider: RepositoryProvider;
	authMethod: RepositoryAuthMethod;
	authUser?: string;
	authToken?: string;
}

export function listRepositories(): Promise<Repository[]> {
	return fetcher<Repository[]>("/repositories", "GET");
}

export async function createRepository(data: CreateRepositoryRequest): Promise<Repository> {
	const res = await fetcher<Repository>("/repositories", "POST", data);
	await mutate(`${API_BASE}/repositories`);
	return res;
}

export function testRepositoryConnection(data: TestConnectionRequest): Promise<void> {
	return fetcher("/repositories/test-connection", "POST", data);
}

export async function deleteRepository(id: string): Promise<void> {
	await fetcher(`/repositories/${id}`, "DELETE");
	await mutate(`${API_BASE}/repositories`);
}

export async function updateRepository(
	id: string,
	data: UpdateRepositoryRequest,
): Promise<Repository> {
	const res = await fetcher<Repository>(`/repositories/${id}`, "PUT", data);
	await mutate(`${API_BASE}/repositories`);
	return res;
}

export function getGitProviderIconPath(provider: RepositoryProvider): string {
	const iconName = provider === "generic" ? "git" : provider;

	return `/assets/icons/${iconName}.svg`;
}

export function getGitProviderIconClass(provider: RepositoryProvider): string {
	return provider === "github" ? "dark:invert" : "";
}
