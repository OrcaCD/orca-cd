import { test, expect, type Browser, type Locator, type Page } from "@playwright/test";
import { APP_URL } from "./const.ts";
import { api } from "./helpers/api.ts";
import { gotoApp } from "./helpers/navigation.ts";
import { OIDC_MOCK_ISSUER, registerMockClient, signInAtMock } from "./helpers/oidc-mock.ts";
import {
	callbackPath,
	clickProvider,
	createMockUser,
	createProvider,
	deleteProviders,
	getProfile,
	listProviders,
	providerButton,
	uniqueId,
	updateProvider,
} from "./helpers/oidc.ts";

const DISCOVERY_ERROR = "failed to discover OIDC provider at the given issuer URL";
const UNREACHABLE_ISSUER = `${OIDC_MOCK_ISSUER}/nothing-here`;

const createdIds: string[] = [];

test.afterAll(async ({ browser }) => {
	const context = await browser.newContext({ storageState: "playwright/.auth/user.json" });
	try {
		const page = await context.newPage();
		await page.goto(`${APP_URL}/api/v1/health`);
		await deleteProviders(page, createdIds);
	} catch {
		// Leftover providers don't affect other tests, every test uses its own
	} finally {
		await context.close();
	}
});

async function seedProvider(page: Page, options: Parameters<typeof createProvider>[1] = {}) {
	await page.goto(`${APP_URL}/api/v1/health`);
	const provider = await createProvider(page, options);
	createdIds.push(provider.id);
	return provider;
}

function newSignedOutContext(browser: Browser) {
	return browser.newContext({ baseURL: APP_URL, storageState: { cookies: [], origins: [] } });
}

function providerCard(page: Page, name: string) {
	return page.locator("[data-slot='card']", { hasText: name });
}

async function openCardMenu(page: Page, name: string, item: "Edit" | "Delete") {
	await providerCard(page, name).getByRole("button", { name: "Actions" }).click();
	await page.getByRole("menu").getByRole("button", { name: item }).click();
}

async function openProviders(page: Page) {
	await gotoApp(page, "/admin/oidc-providers");
	await expect(page.getByRole("button", { name: "Add Provider" })).toBeVisible();
}

async function openAddDialog(page: Page) {
	await openProviders(page);
	await page.getByRole("button", { name: "Add Provider" }).click();
	const dialog = page.getByRole("dialog").filter({ hasText: "Configure a new OpenID Connect" });
	await expect(dialog).toBeVisible();
	return dialog;
}

async function openEditDialog(page: Page, name: string) {
	await openProviders(page);
	await openCardMenu(page, name, "Edit");
	const dialog = page.getByRole("dialog").filter({ hasText: "Update the OIDC provider" });
	await expect(dialog).toBeVisible();
	return dialog;
}

async function fillProvider(
	dialog: Locator,
	values: {
		name?: string;
		issuerUrl?: string;
		clientId?: string;
		clientSecret?: string;
		scopes?: string;
	},
) {
	for (const [field, value] of Object.entries(values)) {
		await dialog.locator(`#${field}`).fill(value);
	}
}

const OPTIONS = {
	enabled: "Enabled",
	requireVerifiedEmail: "Require verified emails",
	autoSignup: "Allow auto signup",
} as const;

async function setOptions(
	dialog: Locator,
	options: Partial<Record<keyof typeof OPTIONS, boolean>>,
) {
	for (const [option, checked] of Object.entries(options)) {
		await dialog
			.getByRole("checkbox", { name: OPTIONS[option as keyof typeof OPTIONS], exact: true })
			.setChecked(checked);
	}
}

async function findProvider(page: Page, name: string) {
	return (await listProviders(page)).find((provider) => provider.name === name);
}

test.describe("Adding a provider", () => {
	test("validates the form before sending it", async ({ page }) => {
		const clientId = uniqueId("ui-invalid");
		const dialog = await openAddDialog(page);

		await dialog.getByRole("button", { name: "Create Provider" }).click();
		await expect(dialog.getByText("Name is required")).toBeVisible();
		await expect(dialog.getByText("Issuer URL must be a valid URL")).toBeVisible();
		await expect(dialog.getByText("Client Id is required")).toBeVisible();
		await expect(dialog.getByText("Client secret is required")).toBeVisible();

		await fillProvider(dialog, {
			name: "x".repeat(101),
			issuerUrl: "ftp://example.com",
			clientId,
			clientSecret: "secret",
		});
		await dialog.getByRole("button", { name: "Create Provider" }).click();
		await expect(dialog.getByText("Name must be at most 100 characters")).toBeVisible();
		await expect(dialog.getByText("Issuer URL must be a valid URL")).toBeVisible();
		await expect(dialog.getByText("Name is required")).toHaveCount(0);
		await expect(dialog.getByText("Client Id is required")).toHaveCount(0);

		expect((await listProviders(page)).some((provider) => provider.clientId === clientId)).toBe(
			false,
		);
	});

	test("starts with the safe defaults", async ({ page }) => {
		const dialog = await openAddDialog(page);

		for (const label of Object.values(OPTIONS)) {
			await expect(dialog.getByRole("checkbox", { name: label, exact: true })).toBeChecked();
		}
		await expect(dialog.locator("#clientSecret")).toHaveAttribute("type", "password");
	});

	test("discards the input when canceled", async ({ page }) => {
		const name = uniqueId("UI Canceled");
		const dialog = await openAddDialog(page);

		await fillProvider(dialog, { name });
		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).toBeHidden();
		expect(await findProvider(page, name)).toBeUndefined();
	});

	test("shows an error if the issuer can not be discovered", async ({ page }) => {
		const name = uniqueId("UI Unreachable");
		const dialog = await openAddDialog(page);

		await fillProvider(dialog, {
			name,
			issuerUrl: UNREACHABLE_ISSUER,
			clientId: "client",
			clientSecret: "secret",
		});
		await dialog.getByRole("button", { name: "Create Provider" }).click();

		await expect(page.getByText(DISCOVERY_ERROR)).toBeVisible();
		await expect(dialog).toBeVisible();
		expect(await findProvider(page, name)).toBeUndefined();
	});

	test("creates a provider with custom options", async ({ page }) => {
		const name = uniqueId("UI Custom");
		const clientId = uniqueId("ui-client");
		const dialog = await openAddDialog(page);

		await fillProvider(dialog, {
			name,
			issuerUrl: OIDC_MOCK_ISSUER,
			clientId,
			clientSecret: "ui-secret",
			scopes: "groups",
		});
		await setOptions(dialog, { requireVerifiedEmail: false, autoSignup: false });
		await dialog.getByRole("button", { name: "Create Provider" }).click();

		await expect(page.getByText("Provider created")).toBeVisible();
		await expect(dialog).toBeHidden();

		const provider = (await findProvider(page, name))!;
		createdIds.push(provider.id);
		expect(provider).toMatchObject({
			issuerUrl: OIDC_MOCK_ISSUER,
			clientId,
			scopes: "groups",
			enabled: true,
			requireVerifiedEmail: false,
			autoSignup: false,
			callbackUrl: `${APP_URL}${callbackPath(provider.id)}`,
		});
		// The secret is write-only
		expect(JSON.stringify(provider)).not.toContain("ui-secret");

		const card = providerCard(page, name);
		await expect(card).toContainText(OIDC_MOCK_ISSUER);
		await expect(card).toContainText(clientId);
		await expect(card).toContainText("groups");
		await expect(card).toContainText("Enabled");
		await expect(card).toContainText("Unverified email allowed");
		await expect(card).toContainText("No auto signup");
		await expect(card.getByText(provider.callbackUrl)).toBeVisible();
	});

	test("creates a provider that can be used to sign in", async ({ page, browser }) => {
		const name = uniqueId("UI Login");
		const clientId = uniqueId("ui-client");
		const dialog = await openAddDialog(page);

		await fillProvider(dialog, {
			name,
			issuerUrl: OIDC_MOCK_ISSUER,
			clientId,
			clientSecret: "ui-secret",
		});
		await dialog.getByRole("button", { name: "Create Provider" }).click();
		await expect(page.getByText("Provider created")).toBeVisible();

		const provider = (await findProvider(page, name))!;
		createdIds.push(provider.id);
		expect(provider).toMatchObject({
			scopes: "",
			enabled: true,
			requireVerifiedEmail: true,
			autoSignup: true,
		});
		await expect(providerCard(page, name)).toContainText("Verified email required");
		await expect(providerCard(page, name)).toContainText("Auto signup");

		await registerMockClient(clientId, "ui-secret", [provider.callbackUrl]);
		const user = await createMockUser();
		const context = await newSignedOutContext(browser);
		try {
			const loginPage = await context.newPage();
			await clickProvider(loginPage, provider);
			await signInAtMock(loginPage, user.sub);
			await loginPage.waitForURL("/");
			expect(await getProfile(loginPage)).toMatchObject({ email: user.email, isLocal: false });
		} finally {
			await context.close();
		}
	});
});

test.describe("Editing a provider", () => {
	test("shows the current configuration and keeps the secret", async ({ page }) => {
		const provider = await seedProvider(page, { scopes: "groups" });
		const dialog = await openEditDialog(page, provider.name);

		await expect(dialog.locator("#name")).toHaveValue(provider.name);
		await expect(dialog.locator("#issuerUrl")).toHaveValue(provider.issuerUrl);
		await expect(dialog.locator("#clientId")).toHaveValue(provider.clientId);
		await expect(dialog.locator("#scopes")).toHaveValue("groups");
		await expect(dialog.locator("#clientSecret")).toHaveValue("");
		await expect(dialog.getByText("(leave blank to keep current)")).toBeVisible();
		for (const label of Object.values(OPTIONS)) {
			await expect(dialog.getByRole("checkbox", { name: label, exact: true })).toBeChecked();
		}
	});

	test("updates the provider", async ({ page }) => {
		const provider = await seedProvider(page);
		const newName = uniqueId("UI Renamed");
		const dialog = await openEditDialog(page, provider.name);

		await fillProvider(dialog, { name: newName, scopes: "groups" });
		await setOptions(dialog, { enabled: false, requireVerifiedEmail: false, autoSignup: false });
		await dialog.getByRole("button", { name: "Update Provider" }).click();

		await expect(page.getByText("Provider updated")).toBeVisible();
		await expect(dialog).toBeHidden();

		expect(await findProvider(page, newName)).toMatchObject({
			id: provider.id,
			clientId: provider.clientId,
			scopes: "groups",
			enabled: false,
			requireVerifiedEmail: false,
			autoSignup: false,
		});
		const card = providerCard(page, newName);
		await expect(card).toContainText("Disabled");
		await expect(card).toContainText("Unverified email allowed");
		await expect(card).toContainText("No auto signup");
		await expect(card).toContainText("groups");
	});

	test("validates the form before sending it", async ({ page }) => {
		const provider = await seedProvider(page);
		const dialog = await openEditDialog(page, provider.name);

		await fillProvider(dialog, { name: "", issuerUrl: "", clientId: "" });
		await dialog.getByRole("button", { name: "Update Provider" }).click();

		await expect(dialog.getByText("Name is required")).toBeVisible();
		await expect(dialog.getByText("Issuer URL must be a valid URL")).toBeVisible();
		await expect(dialog.getByText("Client Id is required")).toBeVisible();
		// The secret is optional when editing
		await expect(dialog.getByText("Client secret is required")).toHaveCount(0);
		expect(await findProvider(page, provider.name)).toBeDefined();
	});

	test("rejects an issuer that can not be discovered", async ({ page }) => {
		const provider = await seedProvider(page);
		const dialog = await openEditDialog(page, provider.name);

		await fillProvider(dialog, { name: "Should not be saved", issuerUrl: UNREACHABLE_ISSUER });
		await dialog.getByRole("button", { name: "Update Provider" }).click();

		await expect(page.getByText(DISCOVERY_ERROR)).toBeVisible();
		await expect(dialog).toBeVisible();
		expect(await findProvider(page, provider.name)).toMatchObject({
			id: provider.id,
			issuerUrl: OIDC_MOCK_ISSUER,
		});
	});
});

test.describe("Deleting a provider", () => {
	test("asks for confirmation", async ({ page }) => {
		const provider = await seedProvider(page);
		await openProviders(page);

		await openCardMenu(page, provider.name, "Delete");
		const dialog = page.getByRole("dialog").filter({ hasText: "Are you sure?" });
		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).toBeHidden();
		await expect(providerCard(page, provider.name)).toBeVisible();
		expect(await findProvider(page, provider.name)).toBeDefined();
	});

	test("removes the provider", async ({ page }) => {
		const provider = await seedProvider(page);
		await openProviders(page);

		await openCardMenu(page, provider.name, "Delete");
		await page
			.getByRole("dialog")
			.filter({ hasText: "Are you sure?" })
			.getByRole("button", { name: "Proceed" })
			.click();

		await expect(page.getByText("Provider deleted")).toBeVisible();
		await expect(providerCard(page, provider.name)).toHaveCount(0);
		expect(await findProvider(page, provider.name)).toBeUndefined();
		expect((await api(page, "GET", `/admin/oidc-providers/${provider.id}`)).status).toBe(404);
	});

	test("removes the provider from the login page", async ({ page, browser }) => {
		const provider = await seedProvider(page);
		const context = await newSignedOutContext(browser);
		try {
			const loginPage = await context.newPage();
			await loginPage.goto("/login");
			await expect(providerButton(loginPage, provider)).toBeVisible();

			await deleteProviders(page, [provider.id]);

			await loginPage.reload();
			await expect(loginPage.getByRole("textbox", { name: "email" })).toBeVisible();
			await expect(providerButton(loginPage, provider)).toHaveCount(0);
		} finally {
			await context.close();
		}
	});
});

test.describe("Provider status on the login page", () => {
	test("follows the enabled setting", async ({ page, browser }) => {
		const provider = await seedProvider(page);
		const context = await newSignedOutContext(browser);
		try {
			const loginPage = await context.newPage();
			await loginPage.goto("/login");
			const link = providerButton(loginPage, provider);
			await expect(link).toBeVisible();

			await updateProvider(page, provider, { enabled: false });
			await loginPage.reload();
			await expect(loginPage.getByRole("textbox", { name: "email" })).toBeVisible();
			await expect(link).toHaveCount(0);

			await updateProvider(page, provider, { enabled: true });
			await loginPage.reload();
			await expect(link).toBeVisible();
		} finally {
			await context.close();
		}
	});
});

test.describe("API", () => {
	const valid = (): Record<string, unknown> => ({
		name: uniqueId("API Provider"),
		issuerUrl: OIDC_MOCK_ISSUER,
		clientId: uniqueId("api-client"),
		clientSecret: "api-secret",
	});

	test("requires an admin", async ({ playwright, page }) => {
		const provider = await seedProvider(page);
		const anonymous = await playwright.request.newContext({
			baseURL: APP_URL,
			storageState: { cookies: [], origins: [] },
			extraHTTPHeaders: { Origin: APP_URL },
		});
		try {
			expect((await anonymous.get("/api/v1/admin/oidc-providers")).status()).toBe(401);
			expect((await anonymous.get(`/api/v1/admin/oidc-providers/${provider.id}`)).status()).toBe(
				401,
			);
			expect(
				(await anonymous.post("/api/v1/admin/oidc-providers", { data: valid() })).status(),
			).toBe(401);
			expect(
				(
					await anonymous.put(`/api/v1/admin/oidc-providers/${provider.id}`, { data: valid() })
				).status(),
			).toBe(401);
			expect((await anonymous.delete(`/api/v1/admin/oidc-providers/${provider.id}`)).status()).toBe(
				401,
			);
		} finally {
			await anonymous.dispose();
		}
		expect(await findProvider(page, provider.name)).toBeDefined();
	});

	test("rejects invalid create requests", async ({ page }) => {
		await page.goto(`${APP_URL}/api/v1/health`);
		const create = (body: Record<string, unknown>) =>
			api(page, "POST", "/admin/oidc-providers", { ...valid(), ...body });
		const before = (await listProviders(page)).length;

		expect((await api(page, "POST", "/admin/oidc-providers", {})).status).toBe(400);
		expect((await create({ name: "" })).status).toBe(400);
		expect((await create({ name: "x".repeat(101) })).status).toBe(400);
		expect((await create({ issuerUrl: "" })).status).toBe(400);
		expect((await create({ issuerUrl: "not-a-url" })).status).toBe(400);
		expect((await create({ issuerUrl: "ftp://example.com" })).status).toBe(400);
		expect((await create({ clientId: "" })).status).toBe(400);
		expect((await create({ clientSecret: "" })).status).toBe(400);

		expect((await listProviders(page)).length).toBe(before);
	});

	test("only accepts issuers that serve a discovery document", async ({ page }) => {
		await page.goto(`${APP_URL}/api/v1/health`);
		const issuers = {
			"a missing document": UNREACHABLE_ISSUER,
			"a closed port": "http://host.docker.internal:1",
			"the loopback address of the hub": "http://127.0.0.1:8080",
			"the cloud metadata address": "http://169.254.169.254/latest",
		};

		for (const [description, issuerUrl] of Object.entries(issuers)) {
			const response = await api(page, "POST", "/admin/oidc-providers", { ...valid(), issuerUrl });
			expect(response, description).toEqual({ status: 400, body: { error: DISCOVERY_ERROR } });
		}
	});

	test("enables everything by default", async ({ page }) => {
		const { id } = await seedProvider(page);

		const { status, body } = await api(page, "GET", `/admin/oidc-providers/${id}`);
		expect(status).toBe(200);
		expect(body).toMatchObject({
			scopes: "",
			enabled: true,
			requireVerifiedEmail: true,
			autoSignup: true,
		});
		expect(Object.keys(body)).not.toContain("clientSecret");
	});

	test("keeps the sign-in options that were set on creation", async ({ page }) => {
		const provider = await seedProvider(page, { requireVerifiedEmail: false, autoSignup: false });

		const { status, body } = await api(page, "GET", `/admin/oidc-providers/${provider.id}`);
		expect(status).toBe(200);
		expect(body).toMatchObject({ requireVerifiedEmail: false, autoSignup: false });
		expect(JSON.stringify(body)).not.toContain(provider.clientSecret);
	});

	test("keeps a provider disabled that was created disabled", async ({ page }) => {
		await page.goto(`${APP_URL}/api/v1/health`);
		const { status, body } = await api(page, "POST", "/admin/oidc-providers", {
			...valid(),
			enabled: false,
		});
		expect(status).toBe(201);
		createdIds.push(body.id);

		expect(body.enabled).toBe(false);
		expect((await findProvider(page, body.name))?.enabled).toBe(false);
	});

	test("rejects invalid update requests", async ({ page }) => {
		const provider = await seedProvider(page);
		const update = (id: string, body: Record<string, unknown>) =>
			api(page, "PUT", `/admin/oidc-providers/${id}`, {
				name: provider.name,
				issuerUrl: provider.issuerUrl,
				clientId: provider.clientId,
				...body,
			});

		expect((await api(page, "PUT", `/admin/oidc-providers/${provider.id}`, {})).status).toBe(400);
		expect((await update(provider.id, { name: "" })).status).toBe(400);
		expect((await update(provider.id, { name: "x".repeat(101) })).status).toBe(400);
		expect((await update(provider.id, { issuerUrl: "ftp://example.com" })).status).toBe(400);
		expect((await update(provider.id, { clientId: "" })).status).toBe(400);
		expect(await update(provider.id, { issuerUrl: UNREACHABLE_ISSUER })).toEqual({
			status: 400,
			body: { error: DISCOVERY_ERROR },
		});
		expect((await update("does-not-exist", {})).status).toBe(404);

		// Nothing of the rejected updates was applied
		expect(await findProvider(page, provider.name)).toMatchObject({
			id: provider.id,
			issuerUrl: OIDC_MOCK_ISSUER,
			clientId: provider.clientId,
		});
	});

	test("answers 404 for unknown providers", async ({ page }) => {
		await page.goto(`${APP_URL}/api/v1/health`);

		expect(await api(page, "GET", "/admin/oidc-providers/does-not-exist")).toEqual({
			status: 404,
			body: { error: "provider not found" },
		});
		expect(await api(page, "DELETE", "/admin/oidc-providers/does-not-exist")).toEqual({
			status: 404,
			body: { error: "provider not found" },
		});
	});

	test("lists the providers", async ({ page }) => {
		const provider = await seedProvider(page);

		expect(await listProviders(page)).toContainEqual(
			expect.objectContaining({ id: provider.id, name: provider.name }),
		);
	});
});
