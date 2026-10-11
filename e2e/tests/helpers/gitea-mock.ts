import { GITEA_MOCK_API_PORT, GITEA_MOCK_CONTROL_URL } from "../const.ts";

export const GITEA_MOCK_HOST_URL = `https://host.docker.internal:${GITEA_MOCK_API_PORT}`;

export interface MockRequest {
	method: string;
	path: string;
	authorized: boolean;
}

export function repoUrl(name: string) {
	return `${GITEA_MOCK_HOST_URL}/${name}`;
}

async function control(path: string, method: string, body?: unknown) {
	const response = await fetch(`${GITEA_MOCK_CONTROL_URL}/__control/repos/${path}`, {
		method,
		headers: { "Content-Type": "application/json" },
		body: body === undefined ? undefined : JSON.stringify(body),
	});
	if (!response.ok) {
		throw new Error(`Gitea mock ${method} ${path} failed: ${response.status}`);
	}
	return response;
}

export async function createRepo(
	name: string,
	options: { branches?: Record<string, { files?: Record<string, string> }>; token?: string } = {},
) {
	await control(name, "PUT", options);
}

export async function commit(
	name: string,
	{
		branch = "main",
		files,
		message,
	}: { branch?: string; files: Record<string, string | null>; message?: string },
) {
	const response = await control(`${name}/files`, "PUT", { branch, files, message });
	return (await response.json()) as { sha: string; message: string };
}

export async function requireToken(name: string, token: string | null) {
	await control(`${name}/token`, "PUT", { token });
}

export async function requests(name: string) {
	return (await (await control(`${name}/requests`, "GET")).json()) as MockRequest[];
}

export async function deleteRepo(name: string) {
	await control(name, "DELETE");
}
