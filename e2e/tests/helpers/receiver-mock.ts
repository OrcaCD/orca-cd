import { expect } from "@playwright/test";
import { RECEIVER_MOCK_API_PORT, RECEIVER_MOCK_CONTROL_URL } from "../const.ts";

export const RECEIVER_MOCK_HOST_URL = `http://host.docker.internal:${RECEIVER_MOCK_API_PORT}`;

export interface ReceivedRequest {
	method: string;
	path: string;
	headers: Record<string, string>;
	body: string;
}

export function hookUrl(name: string) {
	return `${RECEIVER_MOCK_HOST_URL}/hooks/${name}`;
}

async function control(path: string, method: string, body?: unknown) {
	const response = await fetch(`${RECEIVER_MOCK_CONTROL_URL}/__control/hooks/${path}`, {
		method,
		headers: { "Content-Type": "application/json" },
		body: body === undefined ? undefined : JSON.stringify(body),
	});
	if (!response.ok) {
		throw new Error(`Receiver mock ${method} ${path} failed: ${response.status}`);
	}
	return response;
}

export async function receivedRequests(name: string) {
	return (await (await control(name, "GET")).json()) as ReceivedRequest[];
}

export async function receivedMessages(name: string) {
	return (await receivedRequests(name)).map(
		(request) => (JSON.parse(request.body) as { message: string }).message,
	);
}

export async function clearRequests(name: string) {
	await control(name, "DELETE");
}

export async function setStatus(name: string, status: number, location?: string) {
	await control(`${name}/status`, "PUT", { status, location });
}

export async function waitForMessages(name: string, count: number, timeout = 30_000) {
	await expect.poll(async () => (await receivedRequests(name)).length, { timeout }).toBe(count);
	return receivedMessages(name);
}
