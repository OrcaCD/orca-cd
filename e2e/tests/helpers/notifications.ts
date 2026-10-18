import { expect, type Page } from "@playwright/test";
import { api } from "./api.ts";

export interface ApiNotification {
	id: string;
	name: string;
	enabled: boolean;
	allApplications: boolean;
	allAgents: boolean;
	allRepositories: boolean;
	events: string[];
	status: string;
	type: string;
	applicationIds: string[];
	agentIds: string[];
	repositoryIds: string[];
}

export async function listNotifications(page: Page) {
	const { status, body } = await api(page, "GET", "/notifications");
	expect(status).toBe(200);
	return body as ApiNotification[];
}

export async function getNotification(page: Page, name: string) {
	return (await listNotifications(page)).find((notification) => notification.name === name);
}

/** Creates a notification through the API. The hub sends a test message to the receiver first. */
export async function createNotification(page: Page, body: Record<string, unknown>) {
	const response = await api(page, "POST", "/notifications", body);
	expect(response.status).toBe(201);
	return response.body as ApiNotification;
}

export async function deleteNotifications(page: Page, ids: string[]) {
	for (const id of ids) {
		await api(page, "DELETE", `/notifications/${id}`);
	}
}
