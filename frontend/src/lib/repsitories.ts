import { fetcher } from "./api";

export enum RepositoryStatus {
	Connected = 0,
	Error = 1,
}

export interface Repository {
	id: string;
	name: string;
	url: string;
	status: RepositoryStatus;
	lastSync?: Date;
	apps: number;
}

export interface CreateRepositoryRequest {
	name: string;
	url: string;
}

export interface UpdateRepositoryRequest {
	name: string;
	url: string;
}

export function createRepository(data: CreateRepositoryRequest): Promise<Repository> {
	return fetcher<Repository>("/repositories", "POST", data);
}

export function updateRepository(id: string, data: UpdateRepositoryRequest): Promise<Repository> {
	return fetcher<Repository>(`/repositories/${id}`, "PUT", data);
}

export function deleteRepository(id: string): Promise<void> {
	return fetcher(`/repositories/${id}`, "DELETE");
}
