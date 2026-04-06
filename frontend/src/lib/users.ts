import fetcher, { API_BASE } from "./api";

export interface UserDetail {
	id: string;
	name: string;
	email: string;
	role: string;
	hasPassword: boolean;
	createdAt: string;
	updatedAt: string;
}

export interface CreateUserRequest {
	name: string;
	email: string;
	password: string;
	role: string;
}

export interface UpdateUserRequest {
	name: string;
	email: string;
	role: string;
	password?: string;
}

export function createUser(data: CreateUserRequest): Promise<UserDetail> {
	return fetcher<UserDetail>(`${API_BASE}/admin/users`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(data),
	});
}

export function updateUser(id: string, data: UpdateUserRequest): Promise<UserDetail> {
	return fetcher<UserDetail>(`${API_BASE}/admin/users/${id}`, {
		method: "PUT",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(data),
	});
}

export async function deleteUser(id: string): Promise<void> {
	await fetcher(`${API_BASE}/admin/users/${id}`, { method: "DELETE" });
}
