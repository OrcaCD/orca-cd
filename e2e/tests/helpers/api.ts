import { expect, type Page } from "@playwright/test";
import { APP_URL } from "../const.ts";
import { createRepo, repoUrl } from "./gitea-mock.ts";

export interface ApiRepository {
	id: string;
	name: string;
	url: string;
	provider: string;
	authMethod: string;
	syncType: string;
	syncStatus: string;
	pollingIntervalSeconds: number | null;
	lastSyncedAt: string | null;
	lastSyncError: string | null;
	appCount: number;
	webhookSecret?: string;
	webhookUrl?: string;
}

/**
 * Calls the hub API from inside the page, so the session cookie and the Origin header
 * are the same as for the UI.
 */
export async function api(page: Page, method: string, path: string, body?: unknown) {
	if (!page.url().startsWith(APP_URL)) {
		await page.goto(`${APP_URL}/api/v1/health`);
	}
	return page.evaluate(
		async ({ method, path, body }) => {
			const response = await fetch(`/api/v1${path}`, {
				method,
				headers: body === undefined ? undefined : { "Content-Type": "application/json" },
				// oxlint-disable-next-line unicorn/no-invalid-fetch-options
				body: body === undefined ? undefined : JSON.stringify(body),
			});
			const text = await response.text();
			let json: any = null;
			try {
				json = text ? JSON.parse(text) : null;
			} catch {
				json = text;
			}
			return { status: response.status, body: json };
		},
		{ method, path, body },
	);
}

export async function listRepositories(page: Page) {
	const { status, body } = await api(page, "GET", "/repositories");
	expect(status).toBe(200);
	return body as ApiRepository[];
}

export async function seedRepository(
	page: Page,
	name: string,
	extra: Record<string, unknown> = {},
	mock: Parameters<typeof createRepo>[1] = {},
) {
	await createRepo(name, mock);
	const { status, body } = await api(page, "POST", "/repositories", {
		url: repoUrl(name),
		provider: "gitea",
		authMethod: "none",
		syncType: "manual",
		...extra,
	});
	expect(status).toBe(201);
	return body as ApiRepository;
}
