import { randomUUID } from "node:crypto";
import { expect, type BrowserContext, type Page } from "@playwright/test";
import { api } from "./api.ts";
import {
	OIDC_MOCK_ISSUER,
	registerMockClient,
	setMockUser,
	type MockUser,
	type ProfileSource,
} from "./oidc-mock.ts";

export interface ApiOidcProvider {
	id: string;
	name: string;
	issuerUrl: string;
	clientId: string;
	scopes: string;
	enabled: boolean;
	requireVerifiedEmail: boolean;
	autoSignup: boolean;
	callbackUrl: string;
}

export interface ApiAdminUser {
	id: string;
	name: string;
	email: string;
	role: string;
	providers: string[];
}

export interface ApiProfile {
	id: string;
	name: string;
	email: string;
	role: string;
	isLocal: boolean;
	picture?: string;
}

export interface TestProvider extends ApiOidcProvider {
	clientSecret: string;
}

interface ProviderOptions {
	name?: string;
	issuerUrl?: string;
	scopes?: string;
	enabled?: boolean;
	requireVerifiedEmail?: boolean;
	autoSignup?: boolean;
	// The secret the hub is configured with, the identity provider keeps the correct one
	wrongSecret?: boolean;
	profileSource?: ProfileSource;
}

export function uniqueId(prefix: string) {
	return `${prefix}-${randomUUID().slice(0, 8)}`;
}

export async function createMockUser(overrides: MockUser = {}) {
	const sub = uniqueId("user");
	const user: MockUser = {
		email: `${sub}@example.com`,
		email_verified: true,
		name: `Name ${sub}`,
		...overrides,
	};
	await setMockUser(sub, user);
	return { sub, ...user };
}

export async function createProvider(page: Page, options: ProviderOptions = {}) {
	const clientId = uniqueId("client");
	const clientSecret = uniqueId("secret");
	const { wrongSecret, profileSource, ...rest } = options;
	const { status, body } = await api(page, "POST", "/admin/oidc-providers", {
		name: uniqueId("E2E SSO"),
		issuerUrl: OIDC_MOCK_ISSUER,
		clientId,
		clientSecret: wrongSecret ? "not-the-secret" : clientSecret,
		...rest,
	});
	expect(status, JSON.stringify(body)).toBe(201);
	const provider = body as ApiOidcProvider;
	await registerMockClient(clientId, clientSecret, [provider.callbackUrl], profileSource);
	return { ...provider, clientSecret } satisfies TestProvider;
}

export async function updateProvider(
	page: Page,
	provider: ApiOidcProvider,
	changes: Partial<Omit<ApiOidcProvider, "id" | "callbackUrl">> & { clientSecret?: string },
) {
	const { status, body } = await api(page, "PUT", `/admin/oidc-providers/${provider.id}`, {
		name: provider.name,
		issuerUrl: provider.issuerUrl,
		clientId: provider.clientId,
		scopes: provider.scopes,
		enabled: provider.enabled,
		requireVerifiedEmail: provider.requireVerifiedEmail,
		autoSignup: provider.autoSignup,
		...changes,
	});
	expect(status, JSON.stringify(body)).toBe(200);
	return body as ApiOidcProvider;
}

export async function listProviders(page: Page) {
	const { status, body } = await api(page, "GET", "/admin/oidc-providers");
	expect(status).toBe(200);
	return body as ApiOidcProvider[];
}

export async function deleteProviders(page: Page, ids: string[]) {
	for (const id of ids) {
		await api(page, "DELETE", `/admin/oidc-providers/${id}`);
	}
}

export async function listUsers(page: Page) {
	const { status, body } = await api(page, "GET", "/admin/users");
	expect(status).toBe(200);
	return body as ApiAdminUser[];
}

export async function findUsers(page: Page, email: string) {
	return (await listUsers(page)).filter((user) => user.email === email);
}

export async function getProfile(page: Page) {
	const { status, body } = await api(page, "GET", "/auth/profile");
	expect(status).toBe(200);
	return body as ApiProfile;
}

export function authorizePath(providerId: string) {
	return `/api/v1/auth/oidc/${providerId}/authorize`;
}

export function callbackPath(providerId: string) {
	return `/api/v1/auth/oidc/${providerId}/callback`;
}

/**
 * The login endpoints allow 5 requests per client IP and one more every 6 seconds. The hub
 * trusts the forwarded IP of the E2E setup, so every flow can use an IP of its own.
 */
export async function useNewClientIp(context: BrowserContext) {
	const random = (max: number) => Math.floor(Math.random() * max);
	const ip = `198.${18 + random(2)}.${random(256)}.${1 + random(254)}`;
	await context.setExtraHTTPHeaders({ "X-Forwarded-For": ip });
}

export function providerButton(page: Page, provider: Pick<ApiOidcProvider, "name">) {
	return page.getByRole("button", { name: `Continue with ${provider.name}` });
}

export async function clickProvider(page: Page, provider: ApiOidcProvider) {
	await useNewClientIp(page.context());
	// The identity provider remembers the user otherwise and signs the next login in without asking
	await page.context().clearCookies({ domain: "host.docker.internal" });
	await page.goto("/login");
	await providerButton(page, provider).click();
}
