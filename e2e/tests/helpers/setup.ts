import { expect, type Browser, type Page } from "@playwright/test";
import { APP_URL, HUB_WS_URL } from "../const.ts";
import { startAgent } from "./agent-container.ts";
import { api } from "./api.ts";

export interface ApiAgent {
	id: string;
	name: string;
	status: string;
	authToken?: string;
}

export interface ApiApplication {
	id: string;
	name: string;
	agentId: string;
	repositoryId: string;
	syncStatus: string;
	healthStatus: string;
	branch: string;
	path: string;
	commit: string;
	commitMessage: string;
	lastSyncedAt: string | null;
	lastSyncError?: string;
	composeFile: string;
	imagePollEnabled: boolean;
	imagePollIntervalSeconds: number;
	deleteOldImages: boolean;
	imageWebhookEnabled: boolean;
	imageWebhookUrl?: string;
}

export interface ApiApplicationEvent {
	id: string;
	type: string;
	source: string;
	status: string;
	commitHash: string | null;
	errorMessage: string | null;
}

export function newAuthenticatedContext(browser: Browser) {
	return browser.newContext({ baseURL: APP_URL, storageState: "playwright/.auth/user.json" });
}

export async function createOnlineAgent(page: Page, name: string) {
	const created = await api(page, "POST", "/agents", { name });
	expect(created.status).toBe(201);
	const agent = created.body as ApiAgent;

	startAgent({ hubUrl: HUB_WS_URL, authToken: agent.authToken });
	await expect
		.poll(async () => ((await api(page, "GET", `/agents/${agent.id}`)).body as ApiAgent).status, {
			timeout: 60_000,
		})
		.toBe("online");
	return agent;
}

export async function getApplication(page: Page, id: string) {
	const { status, body } = await api(page, "GET", `/applications/${id}`);
	expect(status).toBe(200);
	return body as ApiApplication;
}

export async function listApplications(page: Page) {
	const { status, body } = await api(page, "GET", "/applications");
	expect(status).toBe(200);
	return body as ApiApplication[];
}

export async function applicationEvents(page: Page, id: string) {
	const { status, body } = await api(page, "GET", `/applications/${id}/events?limit=100`);
	expect(status).toBe(200);
	return (body as { items: ApiApplicationEvent[] }).items;
}

export async function waitForApplication(
	page: Page,
	id: string,
	predicate: (application: ApiApplication) => boolean,
	{ timeout = 60_000, message }: { timeout?: number; message?: string } = {},
) {
	let latest: ApiApplication | undefined;
	await expect
		.poll(
			async () => {
				latest = await getApplication(page, id);
				return predicate(latest);
			},
			{ timeout, message },
		)
		.toBe(true);
	return latest!;
}
