import { test, expect, type Locator, type Page } from "@playwright/test";
import { APP_URL } from "./const.ts";
import { api } from "./helpers/api.ts";
import { gotoApp } from "./helpers/navigation.ts";
import {
	createNotification,
	deleteNotifications,
	getNotification,
	listNotifications,
} from "./helpers/notifications.ts";
import {
	clearRequests,
	hookUrl,
	receivedMessages,
	receivedRequests,
	setStatus,
	waitForMessages,
} from "./helpers/receiver-mock.ts";

const WEBHOOK_NAME = "E2E Webhook Notification";
const CUSTOM_NAME = "E2E Custom Notification";
const WEBHOOK_HOOK = "ui-webhook";
const CUSTOM_HOOK = "ui-custom";
const FAILING_HOOK = "ui-failing";
const API_HOOK = "api-validation";
const SSRF_HOOK = "ssrf";
const DEFAULT_TEST_MESSAGE = "This is a test notification from OrcaCD.";
const SECRET_HEADER_VALUE = "Bearer e2e-secret-token";

const webhookConfig = (hook: string, headers?: Record<string, string>) =>
	JSON.stringify({ webhookUrl: hookUrl(hook), headers });
const customConfig = (hook: string) =>
	`generic://host.docker.internal:${new URL(hookUrl(hook)).port}/hooks/${hook}?disabletls=yes&template=json`;

test.describe.configure({ mode: "serial" });

const createdIds: string[] = [];

test.beforeAll(async () => {
	for (const hook of [WEBHOOK_HOOK, CUSTOM_HOOK, FAILING_HOOK, API_HOOK, SSRF_HOOK]) {
		await clearRequests(hook);
		await setStatus(hook, 200);
	}
});

test.afterAll(async ({ browser }) => {
	const context = await browser.newContext({ storageState: "playwright/.auth/user.json" });
	try {
		const page = await context.newPage();
		await page.goto(`${APP_URL}/api/v1/health`);
		await deleteNotifications(page, createdIds);
	} catch {
		// Leftover notifications don't affect other tests, they are not assigned to any resource
	} finally {
		await context.close();
	}
});

function notificationCard(page: Page, name: string) {
	return page.locator("[data-slot='card']", { hasText: name });
}

async function openCardMenu(page: Page, name: string) {
	await notificationCard(page, name).getByRole("button", { name: "Card actions" }).click();
}

async function openNotifications(page: Page) {
	await gotoApp(page, "/notifications");
	await expect(page.getByRole("heading", { name: "Notifications" })).toBeVisible();
}

async function selectType(page: Page, dialog: Locator, label: string) {
	await dialog.locator("#type").click();
	await page.getByRole("option", { name: label, exact: true }).click();
}

async function startWizard(page: Page, name: string, type: string) {
	await openNotifications(page);
	await page.getByRole("button", { name: "Add Notification" }).click();
	const dialog = page
		.getByRole("dialog")
		.filter({ hasText: "Create a new notification endpoint." });
	await dialog.locator("#name").fill(name);
	await selectType(page, dialog, type);
	await dialog.getByRole("button", { name: "Next" }).click();
	return dialog;
}

async function toProviderStep(dialog: Locator) {
	await dialog.getByRole("button", { name: "Next" }).click();
	await expect(dialog.getByText(/A test notification will be sent/)).toBeVisible();
}

async function submitWizard(dialog: Locator) {
	await dialog.getByRole("button", { name: "Add Notification" }).click();
}

async function deselectAllEvents(dialog: Locator) {
	const tabs = { Applications: 7, Agents: 2, Repositories: 2 };
	for (const [tab, count] of Object.entries(tabs)) {
		await dialog.getByRole("tab", { name: new RegExp(tab) }).click();
		await expect(dialog.getByRole("checkbox")).toHaveCount(count);
		for (const checkbox of await dialog.getByRole("checkbox").all()) {
			await checkbox.uncheck();
		}
	}
}

test.describe("API validation", () => {
	const create = (page: Page, body: Record<string, unknown>) =>
		api(page, "POST", "/notifications", {
			name: "E2E Invalid",
			type: "webhook",
			config: webhookConfig(API_HOOK),
			...body,
		});

	test("rejects invalid create requests", async ({ page }) => {
		await page.goto(`${APP_URL}/api/v1/health`);

		expect(await create(page, { name: "" })).toMatchObject({ status: 400 });
		expect(await create(page, { name: "   " })).toMatchObject({
			status: 400,
			body: { error: "invalid name: must not be empty" },
		});
		expect(await create(page, { name: "x".repeat(129) })).toMatchObject({ status: 400 });
		expect(await create(page, { type: "pager" })).toMatchObject({
			status: 400,
			body: { error: "invalid type" },
		});
		expect(
			await create(page, { config: JSON.stringify({ webhookUrl: "ftp://example.com" }) }),
		).toEqual({
			status: 400,
			body: { error: "invalid config: webhook URL must use http or https" },
		});
		expect(await create(page, { config: JSON.stringify({ webhookUrl: "" }) })).toMatchObject({
			status: 400,
		});
		expect(await create(page, { type: "custom", config: "no-scheme" })).toMatchObject({
			status: 400,
		});
		expect(await create(page, { events: [] })).toMatchObject({ status: 400 });
		expect(await create(page, { events: ["unknown.event"] })).toMatchObject({ status: 400 });
		expect(await create(page, { applicationIds: ["missing"] })).toEqual({
			status: 400,
			body: { error: "application not found: missing" },
		});
		expect(await create(page, { agentIds: ["missing"] })).toMatchObject({ status: 400 });
		expect(await create(page, { repositoryIds: ["missing"] })).toMatchObject({ status: 400 });

		// Validation happens before the test message is sent
		expect(await receivedRequests(API_HOOK)).toHaveLength(0);
		expect((await listNotifications(page)).filter((item) => item.name === "E2E Invalid")).toEqual(
			[],
		);
	});

	test("rejects invalid update requests", async ({ page }) => {
		await page.goto(`${APP_URL}/api/v1/health`);
		const notification = await createNotification(page, {
			name: "E2E API Update",
			type: "webhook",
			config: webhookConfig(API_HOOK),
		});
		createdIds.push(notification.id);
		const valid = {
			enabled: true,
			allApplications: false,
			allAgents: false,
			allRepositories: false,
			events: ["agent.offline"],
		};
		const update = (id: string, body: Record<string, unknown>) =>
			api(page, "PUT", `/notifications/${id}`, { ...valid, ...body });

		expect((await update(notification.id, { events: [] })).status).toBe(400);
		expect((await update(notification.id, { events: ["unknown.event"] })).status).toBe(400);
		expect((await update(notification.id, { applicationIds: ["missing"] })).status).toBe(400);
		expect((await api(page, "PUT", `/notifications/${notification.id}`, {})).status).toBe(400);
		expect((await update("missing", {})).status).toBe(404);

		expect((await api(page, "POST", "/notifications/missing/test")).status).toBe(404);
		expect((await api(page, "DELETE", "/notifications/missing")).status).toBe(404);

		// Nothing of the rejected updates was applied
		expect(await getNotification(page, "E2E API Update")).toMatchObject({
			enabled: true,
			events: notification.events,
		});
		await deleteNotifications(page, [notification.id]);
	});
});

test.describe("Adding a notification", () => {
	test("the list is empty at first", async ({ page }) => {
		await openNotifications(page);
		expect(await listNotifications(page)).toEqual([]);
		await expect(page.getByText("No notifications found")).toBeVisible();
	});

	test("requires a name and at least one event", async ({ page }) => {
		const dialog = await startWizard(page, "", "Webhook");
		await expect(dialog.getByText("Name is required")).toBeVisible();
		await expect(dialog.locator("#name")).toBeVisible();

		await dialog.locator("#name").fill("x".repeat(129));
		await dialog.getByRole("button", { name: "Next" }).click();
		await expect(dialog.getByText("Name must be at most 128 characters")).toBeVisible();

		await dialog.locator("#name").fill(WEBHOOK_NAME);
		await dialog.getByRole("button", { name: "Next" }).click();
		await expect(dialog.getByText("Choose which events trigger this notification.")).toBeVisible();

		await deselectAllEvents(dialog);
		await dialog.getByRole("button", { name: "Next" }).click();
		await expect(dialog.getByText("Select at least one event.")).toBeVisible();
		await expect(dialog.getByText(/A test notification will be sent/)).toHaveCount(0);
	});

	test("validates the webhook fields", async ({ page }) => {
		const dialog = await startWizard(page, WEBHOOK_NAME, "Webhook");
		await toProviderStep(dialog);

		await submitWizard(dialog);
		await expect(dialog.getByText("Webhook URL is required")).toBeVisible();

		await dialog.locator("#webhookUrl").fill("ftp://example.com/hook");
		await submitWizard(dialog);
		await expect(dialog.getByText("Webhook URL must be a valid HTTP or HTTPS URL")).toBeVisible();

		await dialog.locator("#webhookUrl").fill(hookUrl(WEBHOOK_HOOK));
		await dialog.locator("#webhookHeaders").fill("this line has no colon");
		await submitWizard(dialog);
		await expect(
			dialog.getByText("Headers must be one per line in Name: value format"),
		).toBeVisible();

		expect(await receivedRequests(WEBHOOK_HOOK)).toHaveLength(0);
		expect(await listNotifications(page)).toEqual([]);
	});

	test("validates the custom Shoutrrr URL", async ({ page }) => {
		const dialog = await startWizard(page, CUSTOM_NAME, "Custom");
		await toProviderStep(dialog);

		await submitWizard(dialog);
		await expect(dialog.getByText("Shoutrrr URL is required")).toBeVisible();

		// Shoutrrr URLs use service schemes, plain http(s) URLs belong to the webhook type
		await dialog.locator("#customShoutrrrUrl").fill(hookUrl(CUSTOM_HOOK));
		await submitWizard(dialog);
		await expect(
			dialog.getByText("Shoutrrr URL must be a valid URL with a service scheme"),
		).toBeVisible();

		expect(await listNotifications(page)).toEqual([]);
	});

	test("shows why a notification can't be saved when the delivery fails", async ({ page }) => {
		await setStatus(FAILING_HOOK, 500);
		const dialog = await startWizard(page, "E2E Failing Notification", "Webhook");
		await toProviderStep(dialog);
		await dialog.locator("#webhookUrl").fill(hookUrl(FAILING_HOOK));
		await submitWizard(dialog);

		await expect(dialog.getByText("Failed to save notification")).toBeVisible();
		await expect(dialog.getByText(/notification dispatch failed.*status code: 500/i)).toBeVisible();
		// Credentials and paths of the target never end up in the message
		await expect(dialog.getByText(/hooks\/ui-failing/)).toHaveCount(0);
		expect(await receivedRequests(FAILING_HOOK)).toHaveLength(1);
		expect(await listNotifications(page)).toEqual([]);

		await dialog.getByRole("button", { name: "Cancel" }).click();
		await expect(page.getByText("No notifications found")).toBeVisible();
		await setStatus(FAILING_HOOK, 200);
	});

	test("creates a webhook notification", async ({ page }) => {
		const dialog = await startWizard(page, WEBHOOK_NAME, "Webhook");
		await toProviderStep(dialog);
		await dialog.locator("#webhookUrl").fill(hookUrl(WEBHOOK_HOOK));
		await dialog.locator("#webhookHeaders").fill(`Authorization: ${SECRET_HEADER_VALUE}`);
		await submitWizard(dialog);

		await expect(page.getByText("Notification created")).toBeVisible();
		await expect(page.locator("#name")).toHaveCount(0);

		const notification = (await getNotification(page, WEBHOOK_NAME))!;
		createdIds.push(notification.id);
		expect(notification).toMatchObject({
			type: "webhook",
			enabled: true,
			status: "success",
			allApplications: false,
			applicationIds: [],
		});
		expect(notification.events).toHaveLength(11);

		const [request] = await receivedRequests(WEBHOOK_HOOK);
		expect(request.method).toBe("POST");
		expect(request.headers["authorization"]).toBe(SECRET_HEADER_VALUE);
		expect(JSON.parse(request.body)).toEqual({ message: DEFAULT_TEST_MESSAGE });

		const card = notificationCard(page, WEBHOOK_NAME);
		await expect(card).toBeVisible();
		await expect(card.getByText("Success", { exact: true })).toBeVisible();
		await expect(card.getByText("Webhook", { exact: true })).toBeVisible();
		await expect(card.getByText("Enabled", { exact: true })).toBeVisible();
		await expect(page.getByText("No notifications found")).toHaveCount(0);
	});

	test("creates a custom notification with only some events", async ({ page }) => {
		const dialog = await startWizard(page, CUSTOM_NAME, "Custom");
		await dialog.getByRole("checkbox", { name: "Deployment succeeded" }).uncheck();
		await dialog.getByRole("tab", { name: /Agents/ }).click();
		await dialog.getByRole("checkbox", { name: "Back online" }).uncheck();
		await toProviderStep(dialog);
		await dialog.locator("#customShoutrrrUrl").fill(customConfig(CUSTOM_HOOK));
		await submitWizard(dialog);

		await expect(page.getByText("Notification created")).toBeVisible();
		const notification = (await getNotification(page, CUSTOM_NAME))!;
		createdIds.push(notification.id);
		expect(notification).toMatchObject({ type: "custom", enabled: true, status: "success" });
		expect(notification.events).toHaveLength(9);
		expect(notification.events).not.toContain("application.deployment.succeeded");
		expect(notification.events).not.toContain("agent.online");

		expect(await receivedMessages(CUSTOM_HOOK)).toEqual([DEFAULT_TEST_MESSAGE]);
		await expect(
			notificationCard(page, CUSTOM_NAME).getByText("Custom", { exact: true }),
		).toBeVisible();
	});

	test("never returns the configuration", async ({ page }) => {
		await page.goto(`${APP_URL}/api/v1/health`);
		const { body } = await api(page, "GET", "/notifications");
		const raw = JSON.stringify(body);
		expect(raw).not.toContain(SECRET_HEADER_VALUE);
		expect(raw).not.toContain("e2e-secret-token");
		expect(raw).not.toContain(WEBHOOK_HOOK);
		expect(raw).not.toContain("generic://");
		for (const notification of body as Record<string, unknown>[]) {
			expect(notification).not.toHaveProperty("config");
		}
	});
});

// The e2e hub only allows the private ranges 10/8, 172.16/12 and 192.168/16 (ALLOWED_INTERNAL_IPS),
// which is how it reaches the receiver on the Docker host. Everything else private stays blocked.
test.describe("SSRF protection", () => {
	const blockedHosts = [
		{ title: "loopback", host: "127.0.0.1" },
		{ title: "IPv6 loopback", host: "[::1]" },
		{ title: "the cloud metadata address", host: "169.254.169.254" },
		{ title: "the shared address space", host: "100.64.0.1" },
	];

	for (const { title, host } of blockedHosts) {
		test(`blocks a webhook to ${title}`, async ({ page }) => {
			await page.goto(`${APP_URL}/api/v1/health`);
			const response = await api(page, "POST", "/notifications", {
				name: "E2E SSRF Webhook",
				type: "webhook",
				config: JSON.stringify({
					webhookUrl: `http://${host}:${new URL(hookUrl(SSRF_HOOK)).port}/hooks/${SSRF_HOOK}`,
				}),
			});

			expect(response.status).toBe(502);
			expect(response.body.error).toMatch(/SSRF detected/);
			expect(await getNotification(page, "E2E SSRF Webhook")).toBeUndefined();
			expect(await receivedRequests(SSRF_HOOK)).toHaveLength(0);
		});
	}

	test("blocks a custom Shoutrrr URL to a private address", async ({ page }) => {
		await page.goto(`${APP_URL}/api/v1/health`);
		const response = await api(page, "POST", "/notifications", {
			name: "E2E SSRF Custom",
			type: "custom",
			config: "generic://169.254.169.254/latest/meta-data?disabletls=yes&template=json",
		});

		expect(response.status).toBe(502);
		expect(response.body.error).toMatch(/SSRF detected/);
		expect(await getNotification(page, "E2E SSRF Custom")).toBeUndefined();
	});

	test("shows the block when creating it in the UI", async ({ page }) => {
		const dialog = await startWizard(page, "E2E SSRF UI", "Webhook");
		await toProviderStep(dialog);
		await dialog.locator("#webhookUrl").fill("http://169.254.169.254/latest/meta-data");
		await submitWizard(dialog);

		await expect(dialog.getByText("Failed to save notification")).toBeVisible();
		await expect(dialog.getByText(/SSRF detected/)).toBeVisible();
		expect(await getNotification(page, "E2E SSRF UI")).toBeUndefined();
	});

	test("does not follow a redirect to a private address", async ({ page }) => {
		await setStatus(SSRF_HOOK, 302, "http://169.254.169.254/latest/meta-data");
		try {
			await page.goto(`${APP_URL}/api/v1/health`);
			const response = await api(page, "POST", "/notifications", {
				name: "E2E SSRF Redirect",
				type: "webhook",
				config: webhookConfig(SSRF_HOOK),
			});

			expect(await receivedRequests(SSRF_HOOK)).toHaveLength(1);
			expect(response.status).toBe(502);
			expect(response.body.error).toMatch(/SSRF detected: redirect to 169\.254\.169\.254/);
			expect(await getNotification(page, "E2E SSRF Redirect")).toBeUndefined();
		} finally {
			await setStatus(SSRF_HOOK, 200);
		}
	});

	test("still delivers to the allowed internal receiver", async ({ page }) => {
		await clearRequests(SSRF_HOOK);
		await page.goto(`${APP_URL}/api/v1/health`);
		const notification = await createNotification(page, {
			name: "E2E SSRF Allowed",
			type: "webhook",
			config: webhookConfig(SSRF_HOOK),
		});
		createdIds.push(notification.id);

		expect(notification.status).toBe("success");
		expect(await receivedMessages(SSRF_HOOK)).toEqual([DEFAULT_TEST_MESSAGE]);
		await deleteNotifications(page, [notification.id]);
	});
});

test.describe("Test delivery", () => {
	test("delivers the test message to the receiver", async ({ page }) => {
		await clearRequests(WEBHOOK_HOOK);
		await openNotifications(page);
		await openCardMenu(page, WEBHOOK_NAME);
		await page.getByRole("menuitem", { name: "Send test" }).click();

		await expect(page.getByText("Test notification sent")).toBeVisible();
		const [request] = await receivedRequests(WEBHOOK_HOOK);
		expect(request.headers["authorization"]).toBe(SECRET_HEADER_VALUE);
		expect(JSON.parse(request.body)).toEqual({ message: DEFAULT_TEST_MESSAGE });
		await expect(
			notificationCard(page, WEBHOOK_NAME).getByText("Success", { exact: true }),
		).toBeVisible();
	});

	test("uses a custom message", async ({ page }) => {
		await clearRequests(WEBHOOK_HOOK);
		await page.goto(`${APP_URL}/api/v1/health`);
		const notification = (await getNotification(page, WEBHOOK_NAME))!;

		const response = await api(page, "POST", `/notifications/${notification.id}/test`, {
			message: "Hello from the e2e tests",
		});
		expect(response).toEqual({ status: 200, body: { message: "test notification sent" } });
		expect(await waitForMessages(WEBHOOK_HOOK, 1)).toEqual(["Hello from the e2e tests"]);
	});

	test("shows a failing delivery and recovers once the receiver works again", async ({ page }) => {
		await setStatus(WEBHOOK_HOOK, 500);
		try {
			await openNotifications(page);
			await openCardMenu(page, WEBHOOK_NAME);
			await page.getByRole("menuitem", { name: "Send test" }).click();

			await expect(page.getByText(/notification dispatch failed.*status code: 500/i)).toBeVisible();
			await expect(page.getByText(/hooks\/ui-webhook/)).toHaveCount(0);
			await expect(page.getByText(SECRET_HEADER_VALUE)).toHaveCount(0);
			await expect(
				notificationCard(page, WEBHOOK_NAME).getByText("Error", { exact: true }),
			).toBeVisible();
			expect((await getNotification(page, WEBHOOK_NAME))!.status).toBe("error");
		} finally {
			await setStatus(WEBHOOK_HOOK, 200);
		}

		await openCardMenu(page, WEBHOOK_NAME);
		await page.getByRole("menuitem", { name: "Send test" }).click();
		await expect(page.getByText("Test notification sent")).toBeVisible();
		await expect(
			notificationCard(page, WEBHOOK_NAME).getByText("Success", { exact: true }),
		).toBeVisible();
		expect((await getNotification(page, WEBHOOK_NAME))!.status).toBe("success");
	});

	test("works for a custom notification", async ({ page }) => {
		await clearRequests(CUSTOM_HOOK);
		await openNotifications(page);
		await openCardMenu(page, CUSTOM_NAME);
		await page.getByRole("menuitem", { name: "Send test" }).click();

		await expect(page.getByText("Test notification sent")).toBeVisible();
		expect(await receivedMessages(CUSTOM_HOOK)).toEqual([DEFAULT_TEST_MESSAGE]);
	});
});

test.describe("Editing a notification", () => {
	async function openEditDialog(page: Page, name: string) {
		await openNotifications(page);
		await openCardMenu(page, name);
		await page.getByRole("menu").getByRole("button", { name: "Edit" }).click();
		const dialog = page.getByRole("dialog").filter({ hasText: "Edit Notification" });
		await expect(dialog.getByRole("heading", { name: "Edit Notification" })).toBeVisible();
		return dialog;
	}

	test("only exposes the settings, not the provider configuration", async ({ page }) => {
		const dialog = await openEditDialog(page, WEBHOOK_NAME);

		await expect(dialog.getByRole("switch").first()).toBeChecked();
		await expect(dialog.getByRole("checkbox", { name: "Deployment succeeded" })).toBeChecked();
		await expect(dialog.locator("input[type='url'], input[type='password'], textarea")).toHaveCount(
			0,
		);
		await expect(dialog).not.toContainText(SECRET_HEADER_VALUE);
		await expect(dialog).not.toContainText(WEBHOOK_HOOK);

		await dialog.getByRole("button", { name: "Cancel" }).click();
		await expect(dialog.getByRole("heading", { name: "Edit Notification" })).toHaveCount(0);
	});

	test("disables the notification and changes its events", async ({ page }) => {
		const before = (await getNotification(page, WEBHOOK_NAME))!;
		const dialog = await openEditDialog(page, WEBHOOK_NAME);

		await dialog.getByRole("switch").first().click();
		await dialog.getByRole("checkbox", { name: "Deployment succeeded" }).uncheck();
		await dialog.getByRole("button", { name: "Save Changes" }).click();

		await expect(page.getByText("Notification updated")).toBeVisible();
		const updated = (await getNotification(page, WEBHOOK_NAME))!;
		expect(updated.enabled).toBe(false);
		expect(updated.events).toEqual(
			before.events.filter((event) => event !== "application.deployment.succeeded"),
		);
		expect(updated.status).toBe(before.status);
		await expect(
			notificationCard(page, WEBHOOK_NAME).getByText("Disabled", { exact: true }),
		).toBeVisible();
	});

	test("requires at least one event", async ({ page }) => {
		const dialog = await openEditDialog(page, WEBHOOK_NAME);
		await deselectAllEvents(dialog);
		await dialog.getByRole("button", { name: "Save Changes" }).click();

		await expect(dialog.getByText("Select at least one event.")).toBeVisible();
		await dialog.getByRole("button", { name: "Cancel" }).click();
		expect((await getNotification(page, WEBHOOK_NAME))!.events.length).toBeGreaterThan(0);
	});

	test("assigns every application", async ({ page }) => {
		const dialog = await openEditDialog(page, CUSTOM_NAME);

		const applicationSelect = dialog.getByPlaceholder(/Select applications/);
		await expect(applicationSelect).toBeVisible();
		await dialog.getByRole("switch").nth(1).click();
		await expect(applicationSelect).toHaveCount(0);
		await dialog.getByRole("button", { name: "Save Changes" }).click();

		await expect(page.getByText("Notification updated")).toBeVisible();
		const updated = (await getNotification(page, CUSTOM_NAME))!;
		expect(updated.allApplications).toBe(true);
		expect(updated.applicationIds).toEqual([]);
		await expect(notificationCard(page, CUSTOM_NAME).getByText("All applications")).toBeVisible();
	});
});

test.describe("Listing and removing notifications", () => {
	test("search filters the list", async ({ page }) => {
		await openNotifications(page);
		await expect(notificationCard(page, WEBHOOK_NAME)).toBeVisible();
		await expect(notificationCard(page, CUSTOM_NAME)).toBeVisible();

		await page.getByPlaceholder("Search notifications...").fill("custom");
		await expect(notificationCard(page, CUSTOM_NAME)).toBeVisible();
		await expect(notificationCard(page, WEBHOOK_NAME)).toHaveCount(0);

		await page.getByPlaceholder("Search notifications...").fill("does not exist");
		await expect(page.getByText("No notifications found")).toBeVisible();
	});

	test("the table view shows status, type and state", async ({ page }) => {
		await openNotifications(page);
		await page.locator("[data-slot='toggle-group-item']").nth(1).click();

		const webhookRow = page.getByRole("row").filter({ hasText: WEBHOOK_NAME });
		await expect(webhookRow).toContainText("Success");
		await expect(webhookRow).toContainText("Webhook");
		await expect(webhookRow).toContainText("Disabled");

		const customRow = page.getByRole("row").filter({ hasText: CUSTOM_NAME });
		await expect(customRow).toContainText("Custom");
		await expect(customRow).toContainText("Enabled");
		await expect(customRow).toContainText(/all applications/i);

		await page.locator("[data-slot='toggle-group-item']").nth(0).click();
	});

	test("deleting needs a confirmation", async ({ page }) => {
		await openNotifications(page);
		await openCardMenu(page, CUSTOM_NAME);
		await page.getByRole("menu").getByRole("button", { name: "Delete" }).click();
		const dialog = page.getByRole("dialog").filter({ hasText: "Delete notification?" });
		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).toHaveCount(0);
		await expect(notificationCard(page, CUSTOM_NAME)).toBeVisible();
		expect(await getNotification(page, CUSTOM_NAME)).toBeDefined();
	});

	test("deletes a notification", async ({ page }) => {
		await openNotifications(page);
		const { id } = (await getNotification(page, CUSTOM_NAME))!;
		await openCardMenu(page, CUSTOM_NAME);
		await page.getByRole("menu").getByRole("button", { name: "Delete" }).click();
		await page.getByRole("dialog").getByRole("button", { name: "Proceed" }).click();

		await expect(page.getByText(`Notification ${CUSTOM_NAME} deleted successfully`)).toBeVisible();
		await expect(notificationCard(page, CUSTOM_NAME)).toHaveCount(0);
		expect(await getNotification(page, CUSTOM_NAME)).toBeUndefined();
		await expect(notificationCard(page, WEBHOOK_NAME)).toBeVisible();

		await clearRequests(CUSTOM_HOOK);
		await page.goto(`${APP_URL}/api/v1/health`);
		expect((await api(page, "POST", `/notifications/${id}/test`)).status).toBe(404);
		expect(await receivedRequests(CUSTOM_HOOK)).toHaveLength(0);
	});

	test("deleting the last notification shows the empty state", async ({ page }) => {
		await openNotifications(page);
		await openCardMenu(page, WEBHOOK_NAME);
		await page.getByRole("menu").getByRole("button", { name: "Delete" }).click();
		await page.getByRole("dialog").getByRole("button", { name: "Proceed" }).click();

		await expect(page.getByText(`Notification ${WEBHOOK_NAME} deleted successfully`)).toBeVisible();
		await expect(page.getByText("No notifications found")).toBeVisible();
		expect((await listNotifications(page)).map((item) => item.name)).not.toContain(WEBHOOK_NAME);
	});
});
