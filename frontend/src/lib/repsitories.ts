import fetcher, { API_BASE } from "./api";

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
	return fetcher<Repository>(`${API_BASE}/repositories`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(data),
	});
}

export function updateRepository(id: string, data: UpdateRepositoryRequest): Promise<Repository> {
	return fetcher<Repository>(`${API_BASE}/repositories/${id}`, {
		method: "PUT",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(data),
	});
}

export function deleteRepository(id: string): Promise<void> {
	return fetcher(`${API_BASE}/repositories/${id}`, {
		method: "DELETE",
	});
}
