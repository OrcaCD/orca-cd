import { fetcher } from "./api";

export interface UserDetail {
	id: string;
	name: string;
	email: string;
	role: string;
	providers: string[];
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
	return fetcher<UserDetailWithGeneratedPassword>("/admin/users", "POST", data);
}

export function updateUser(
	id: string,
	data: UpdateUserRequest,
): Promise<UserDetailWithOptionalGeneratedPassword> {
	return fetcher<UserDetailWithOptionalGeneratedPassword>(`/admin/users/${id}`, "PUT", data);
}

export async function deleteUser(id: string): Promise<void> {
	await fetcher(`/admin/users/${id}`, "DELETE");
}
