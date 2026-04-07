import fetcher, { API_BASE } from "./api";

export interface UserDetail {
	id: string;
	name: string;
	email: string;
	role: string;
	hasPassword: boolean;
	passwordChangeRequired: boolean;
	createdAt: string;
	updatedAt: string;
}

export interface UserDetailWithGeneratedPassword extends UserDetail {
	generatedPassword: string;
}

export interface UserDetailWithOptionalGeneratedPassword extends UserDetail {
	generatedPassword?: string;
}

export interface CreateUserRequest {
	name: string;
	email: string;
	role: string;
}

export interface UpdateUserRequest {
	name: string;
	email: string;
	role: string;
	resetPassword?: boolean;
}

export function createUser(data: CreateUserRequest): Promise<UserDetailWithGeneratedPassword> {
	return fetcher<UserDetailWithGeneratedPassword>(`${API_BASE}/admin/users`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(data),
	});
}

export function updateUser(
	id: string,
	data: UpdateUserRequest,
): Promise<UserDetailWithOptionalGeneratedPassword> {
	return fetcher<UserDetailWithOptionalGeneratedPassword>(`${API_BASE}/admin/users/${id}`, {
		method: "PUT",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(data),
	});
}

export async function deleteUser(id: string): Promise<void> {
	await fetcher(`${API_BASE}/admin/users/${id}`, { method: "DELETE" });
}
