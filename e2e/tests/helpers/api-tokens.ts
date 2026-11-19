import { randomBytes } from "node:crypto";
import { expect, request, type APIRequestContext, type Page } from "@playwright/test";
import { APP_URL } from "../const.ts";
import { api } from "./api.ts";
import { gotoApp } from "./navigation.ts";

export interface ApiToken {
	id: string;
	name: string;
	prefix: string;
	scopes: string[];
	expiresAt: string;
	lastUsedAt: string | null;
}

/**
 * Creates a token on the settings page and returns the plain token from the dialog that
 * shows it once. Scopes are "admin" or "resource:action", like in the API.
 */
export async function createApiTokenInUi(page: Page, name: string, scopes: string[]) {
	await gotoApp(page, "/settings/api-tokens");
	await page.getByRole("button", { name: "Create token" }).click();
	const dialog = page.getByRole("dialog");
	await dialog.locator("#name").fill(name);
	for (const scope of scopes) {
		await dialog.locator(`label[for="api-token-scope-${scope.replace(":", "-")}"]`).click();
	}
	await dialog.getByRole("button", { name: "Create token" }).click();

	const created = page.getByRole("dialog").filter({ hasText: "API token created" });
	const token = await created.locator("#created-api-token").inputValue();
	expect(token).toMatch(/^orca_[\w-]{86}$/);
	await created.getByRole("button", { name: "Done" }).click();
	return token;
}

/** A client without the session cookie and without an Origin header, like curl. */
export function tokenClient(token: string): Promise<APIRequestContext> {
	return request.newContext({
		baseURL: APP_URL,
		// The project's storage state with the admin session would be applied otherwise
		storageState: { cookies: [], origins: [] },
		extraHTTPHeaders: { Authorization: `Bearer ${token}` },
	});
}

/** A token that is well-formed but was never created. */
export function unknownToken() {
	return `orca_${randomBytes(64).toString("base64url")}`;
}

export async function listApiTokens(page: Page) {
	const { status, body } = await api(page, "GET", "/api-tokens");
	expect(status).toBe(200);
	return body as ApiToken[];
}

export async function deleteApiTokens(page: Page, names: string[]) {
	for (const token of await listApiTokens(page)) {
		if (names.includes(token.name)) {
			await api(page, "DELETE", `/api-tokens/${token.id}`);
		}
	}
}
