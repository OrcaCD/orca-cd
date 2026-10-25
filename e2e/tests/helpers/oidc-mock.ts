import { expect, type Page } from "@playwright/test";
import { OIDC_MOCK_API_PORT, OIDC_MOCK_CONTROL_URL } from "../const.ts";

export const OIDC_MOCK_ISSUER = `http://host.docker.internal:${OIDC_MOCK_API_PORT}`;

export const OIDC_MOCK_AVATAR_URL = `${OIDC_MOCK_ISSUER}/e2e/avatar.svg`;
export const OIDC_MOCK_NOT_AN_IMAGE_URL = `${OIDC_MOCK_ISSUER}/e2e/not-an-image`;
export const OIDC_MOCK_MISSING_IMAGE_URL = `${OIDC_MOCK_ISSUER}/e2e/missing.png`;

export interface MockUser {
	email?: string;
	email_verified?: boolean;
	name?: string;
	picture?: string;
	groups?: string[];
}

async function control(path: string, method: string, body?: unknown) {
	const response = await fetch(`${OIDC_MOCK_CONTROL_URL}/__control/${path}`, {
		method,
		headers: { "Content-Type": "application/json" },
		body: body === undefined ? undefined : JSON.stringify(body),
	});
	if (!response.ok) {
		throw new Error(`OIDC mock ${method} ${path} failed: ${response.status}`);
	}
	return response;
}

export async function setMockUser(sub: string, user: MockUser) {
	await control(`users/${sub}`, "PUT", user);
}

/** Where the identity provider serves the profile claims, "userinfo" leaves them out of the id_token */
export type ProfileSource = "id_token" | "userinfo";

export async function registerMockClient(
	clientId: string,
	secret: string,
	redirectUris: string[],
	profileSource: ProfileSource = "id_token",
) {
	await control(`clients/${clientId}`, "PUT", { secret, redirectUris, profileSource });
}

/** Changes the state or the path of the redirect to the hub that ends a login with the client */
export async function tamperCallback(
	clientId: string,
	tamper: { state?: string; callbackPath?: string },
) {
	await control(`clients/${clientId}/tamper`, "PUT", tamper);
}

export async function authorizationRequests(clientId: string) {
	return (await (await control(`clients/${clientId}/requests`, "GET")).json()) as Record<
		string,
		string
	>[];
}

export async function signInAtMock(page: Page, sub: string) {
	await expect(page.getByRole("heading", { name: "Mock Identity Provider" })).toBeVisible();
	await page.getByRole("button", { name: `Sign in as ${sub}`, exact: true }).click();
}

export async function denyAtMock(page: Page) {
	await expect(page.getByRole("heading", { name: "Mock Identity Provider" })).toBeVisible();
	await page.getByRole("button", { name: "Deny access" }).click();
}
