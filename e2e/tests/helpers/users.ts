import {
	expect,
	type Browser,
	type BrowserContext,
	type Locator,
	type Page,
} from "@playwright/test";
import { APP_URL } from "../const.ts";
import { api } from "./api.ts";
import { uniqueId, useNewClientIp, type ApiAdminUser } from "./oidc.ts";

export const ACTIVE_PASSWORD = "Active-Password-123!";
export const NEW_PASSWORD = "Brand-New-Password-789!";

export interface TestUser extends ApiAdminUser {
	password: string;
}

const createdUserIds: string[] = [];
const openContexts: BrowserContext[] = [];

export function trackUser(id: string) {
	createdUserIds.push(id);
}

export async function createUser(
	admin: Page,
	overrides: Partial<Pick<ApiAdminUser, "name" | "email" | "role">> = {},
) {
	const id = uniqueId("e2e-user");
	const { status, body } = await api(admin, "POST", "/admin/users", {
		name: `Name ${id}`,
		email: `${id}@example.com`,
		role: "user",
		...overrides,
	});
	expect(status, JSON.stringify(body)).toBe(201);

	const { generatedPassword, ...user } = body;
	trackUser(user.id);
	return { ...user, password: generatedPassword } as TestUser;
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

export async function apiSignIn(page: Page, email: string, password: string) {
	const { status, body } = await api(page, "POST", "/auth/login", { email, password });
	expect(status, JSON.stringify(body)).toBe(200);
}

export async function createActiveUser(
	admin: Page,
	browser: Browser,
	overrides: Parameters<typeof createUser>[1] = {},
) {
	const user = await createUser(admin, overrides);
	const session = await openSession(browser);
	await apiSignIn(session.page, user.email, user.password);

	const { status, body } = await api(session.page, "POST", "/auth/change-password", {
		currentPassword: user.password,
		newPassword: ACTIVE_PASSWORD,
	});
	expect(status, JSON.stringify(body)).toBe(200);

	return { ...user, password: ACTIVE_PASSWORD, ...session };
}

export async function cleanup(admin: Page) {
	for (const id of createdUserIds.splice(0)) {
		await api(admin, "DELETE", `/admin/users/${id}`);
	}
	await Promise.all(openContexts.splice(0).map((context) => context.close()));
}

export async function signInUi(page: Page, email: string, password: string) {
	await page.goto("/login");
	await page.getByRole("textbox", { name: "email" }).fill(email);
	await page.getByRole("textbox", { name: "Password", exact: true }).fill(password);
	await page.locator("form button[type='submit']").click();
}

export async function signOutUi(page: Page) {
	await page
		.getByRole("button")
		.filter({ has: page.locator("[data-slot='avatar']") })
		.click();
	await page.getByRole("menuitem", { name: "Sign Out" }).click();
	await page.waitForURL("/login");
}

export async function fillPasswords(
	scope: Page | Locator,
	values: { current: string; next: string; confirm: string },
) {
	await scope.getByLabel("Current Password", { exact: true }).fill(values.current);
	await scope.getByLabel("New Password", { exact: true }).fill(values.next);
	await scope.getByLabel("Confirm New Password", { exact: true }).fill(values.confirm);
}

export async function expectToast(page: Page, message: string) {
	await expect(page.getByText(message, { exact: true }).first()).toBeVisible();
}
