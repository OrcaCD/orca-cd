import { fetcher } from "./api";
import type { UserRole } from "./roles";

export interface UserDetail {
	id: string;
	name: string;
	email: string;
	roles: UserRole[];
	providers: string[];
	isLocal: boolean;
	createdAt: string;
	updatedAt: string;
}

export interface UserDetailWithEnrollment extends UserDetail {
	enrollmentToken: string;
}

export interface UserDetailWithOptionalEnrollment extends UserDetail {
	enrollmentToken?: string;
}

export interface CreateUserRequest {
	name: string;
	email: string;
	roleIds: string[];
}

export interface UpdateUserRequest {
	name: string;
	email: string;
	roleIds: string[];
	resetPasskeys?: boolean;
}

export function createUser(data: CreateUserRequest): Promise<UserDetailWithEnrollment> {
	return fetcher<UserDetailWithEnrollment>("/admin/users", "POST", data);
}

export function updateUser(
	id: string,
	data: UpdateUserRequest,
): Promise<UserDetailWithOptionalEnrollment> {
	return fetcher<UserDetailWithOptionalEnrollment>(`/admin/users/${id}`, "PUT", data);
}

export async function deleteUser(id: string): Promise<void> {
	await fetcher(`/admin/users/${id}`, "DELETE");
}
