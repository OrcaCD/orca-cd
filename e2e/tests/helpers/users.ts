import { expect, type Browser, type BrowserContext, type Page } from "@playwright/test";
import { APP_URL, USER_ROLE_ID } from "../const.ts";
import { api } from "./api.ts";
import { uniqueId, useNewClientIp, type ApiAdminUser } from "./oidc.ts";

import { enroll, installAuthenticator } from "./passkeys.ts";

export interface TestUser extends ApiAdminUser {
	enrollmentToken: string;
}

const createdUserIds: string[] = [];
const openContexts: BrowserContext[] = [];

// Names the built-in roles of a user, or the role name for custom roles
export function roleKeys(user: Pick<ApiAdminUser, "roles"> | undefined) {
	return user?.roles.map((role) => role.builtIn ?? role.name) ?? [];
}

export function trackUser(id: string) {
	createdUserIds.push(id);
}

export async function createUser(
	admin: Page,
	overrides: Partial<Pick<ApiAdminUser, "name" | "email"> & { roleIds: string[] }> = {},
) {
	const id = uniqueId("e2e-user");
	const { status, body } = await api(admin, "POST", "/admin/users", {
		name: `Name ${id}`,
		email: `${id}@example.com`,
		roleIds: [USER_ROLE_ID],
		...overrides,
	});
	expect(status, JSON.stringify(body)).toBe(201);

	const { enrollmentToken, ...user } = body;
	trackUser(user.id);
	return { ...user, enrollmentToken } as TestUser;
}

export async function openSession(browser: Browser) {
	const context = await browser.newContext({
		baseURL: APP_URL,
		// The project's storage state with the admin session would be applied otherwise
		storageState: { cookies: [], origins: [] },
		locale: "en-US",
		colorScheme: "light",
	});
	openContexts.push(context);
	await useNewClientIp(context);
	return { context, page: await context.newPage() };
}

export async function createActiveUser(
	admin: Page,
	browser: Browser,
	overrides: Parameters<typeof createUser>[1] = {},
) {
	const user = await createUser(admin, overrides);
	const session = await openSession(browser);
	const credential = await enroll(session.page, user.enrollmentToken);
	return { ...user, ...session, credential };
}

export async function cleanup(admin: Page) {
	for (const id of createdUserIds.splice(0)) {
		await api(admin, "DELETE", `/admin/users/${id}`);
	}
	await Promise.all(openContexts.splice(0).map((context) => context.close()));
}

export async function signInUi(page: Page) {
	await installAuthenticator(page);
	await page.goto("/login");
	await page.getByRole("button", { name: "Sign in with a passkey" }).click();
}

export async function signOutUi(page: Page) {
	await page
		.getByRole("button")
		.filter({ has: page.locator("[data-slot='avatar']") })
		.click();
	await page.getByRole("menuitem", { name: "Sign Out" }).click();
	await page.waitForURL("/login");
}

export async function expectToast(page: Page, message: string) {
	await expect(page.getByText(message, { exact: true }).first()).toBeVisible();
}
