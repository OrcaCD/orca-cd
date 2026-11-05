import { test, expect, type Cookie, type Page } from "@playwright/test";
import { DEMO_EMAIL, DEMO_PASSWORD, DEMO_URL } from "./const.ts";
import { api } from "./helpers/api.ts";
import { startDemoHub, stopDemoHub } from "./helpers/demo-hub.ts";
import { expectToast, signOutUi } from "./helpers/users.ts";

const BLOCKED_MESSAGE = "This action is not allowed in demo mode.";
const BANNER_MESSAGE = "Demo mode: data is read-only.";
const REDACTED = "hidden in demo mode";

const WEB_APP = { id: "demo-app-web", name: "Demo Web App" };
const MONITORING_APP = { id: "demo-app-monitoring", name: "Monitoring Stack" };
const WORKER_APP = { id: "demo-app-worker", name: "Background Worker" };

function demoApi(page: Page, method: string, path: string, body?: unknown) {
	return api(page, method, path, body, DEMO_URL);
}

async function names(page: Page, path: string) {
	const { status, body } = await demoApi(page, "GET", path);
	expect(status).toBe(200);
	return (body as { name: string }[]).map((item) => item.name).sort();
}

test.beforeAll(async () => {
	test.setTimeout(120_000);
	await startDemoHub();
});

test.afterAll(() => {
	stopDemoHub();
});

test.describe("signed out", () => {
	test("the login form is prefilled with the demo credentials", async ({ page }) => {
		await page.goto("/login");

		await expect(page).toHaveTitle(/Login/);
		await expect(page.getByRole("textbox", { name: "email" })).toHaveValue(DEMO_EMAIL);
		await expect(page.getByRole("textbox", { name: "Password", exact: true })).toHaveValue(
			DEMO_PASSWORD,
		);

		await page.locator("form button[type='submit']").click();
		await page.waitForURL("/");

		await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
		await expect(page.getByText(BANNER_MESSAGE)).toBeVisible();
	});

	test("the setup status marks the hub as demo and no setup is needed", async ({ page }) => {
		const { status, body } = await demoApi(page, "GET", "/auth/setup");

		expect(status).toBe(200);
		expect(body).toMatchObject({ needsSetup: false, demo: true, localAuthEnabled: true });

		await page.goto("/register");
		await page.waitForURL("/login");
	});

	test("only the public demo credentials sign in", async ({ page }) => {
		const attempts = [
			{ email: DEMO_EMAIL, password: "wrong-password" },
			// Seeded, but without a password
			{ email: "sam@orcacd.dev", password: DEMO_PASSWORD },
			{ email: "does-not-exist@example.com", password: DEMO_PASSWORD },
		];

		for (const attempt of attempts) {
			const { status, body } = await demoApi(page, "POST", "/auth/login", attempt);
			expect(status, attempt.email).toBe(401);
			expect(body).toEqual({ error: "invalid email or password" });
		}
	});

	test("the data requires a login", async ({ page }) => {
		for (const path of ["/auth/profile", "/applications", "/admin/users", "/admin/system-info"]) {
			const { status } = await demoApi(page, "GET", path);
			expect(status, path).toBe(401);
		}
	});

	test("webhooks are rejected", async ({ page }) => {
		for (const path of ["/webhooks/repositories/demo-repository", "/github-actions"]) {
			const { status, body } = await demoApi(page, "POST", path, {});
			expect({ status, body }, path).toEqual({ status: 403, body: BLOCKED_MESSAGE });
		}
	});
});

test.describe("signed in", () => {
	let cookies: Cookie[];

	// One login for all tests, the login endpoint is rate limited
	test.beforeAll(async ({ browser }) => {
		const context = await browser.newContext({ baseURL: DEMO_URL });
		try {
			const page = await context.newPage();
			const { status } = await demoApi(page, "POST", "/auth/login", {
				email: DEMO_EMAIL,
				password: DEMO_PASSWORD,
			});
			expect(status).toBe(200);
			cookies = (await context.storageState()).cookies;
		} finally {
			await context.close();
		}
	});

	test.beforeEach(async ({ context }) => {
		await context.addCookies(cookies);
	});

	test("the demo user is an admin and the pages show the banner", async ({ page }) => {
		const { status, body } = await demoApi(page, "GET", "/auth/profile");
		expect(status).toBe(200);
		expect(body).toMatchObject({
			email: DEMO_EMAIL,
			name: "Demo User",
			isAdmin: true,
			demo: true,
			passwordChangeRequired: false,
		});

		for (const path of ["/", "/applications", "/agents", "/repositories", "/admin/users"]) {
			await page.goto(path);
			await expect(page.getByText(BANNER_MESSAGE), path).toBeVisible();
		}
	});

	test("the seeded data is available", async ({ page }) => {
		expect(await names(page, "/applications")).toEqual(
			[WEB_APP.name, MONITORING_APP.name, WORKER_APP.name].sort(),
		);
		expect(await names(page, "/repositories")).toEqual([
			"OrcaCD/demo-app",
			"OrcaCD/demo-infrastructure",
		]);
		expect(await names(page, "/notifications")).toEqual(["Slack alerts", "Team Discord"]);

		const agents = await demoApi(page, "GET", "/agents");
		expect(agents.status).toBe(200);
		expect(
			(agents.body as { name: string; status: string }[]).map(({ name, status }) => ({
				name,
				status,
			})),
		).toEqual(
			expect.arrayContaining([
				{ name: "Prod Server", status: "online" },
				{ name: "Staging Server", status: "offline" },
			]),
		);

		const users = await demoApi(page, "GET", "/admin/users");
		expect(users.status).toBe(200);
		expect((users.body as { email: string }[]).map((user) => user.email).sort()).toEqual([
			DEMO_EMAIL,
			"sam@orcacd.dev",
		]);

		const audit = await demoApi(page, "GET", "/admin/audit-logs");
		expect(audit.status).toBe(200);
		expect(audit.body.items.length).toBeGreaterThan(0);
	});

	test("the seeded applications are listed", async ({ page }) => {
		await page.goto("/applications");

		for (const app of [WEB_APP, MONITORING_APP, WORKER_APP]) {
			await expect(page.getByText(app.name).first(), app.name).toBeVisible();
		}
	});

	test("notification secrets are not exposed", async ({ page }) => {
		const { status, body } = await demoApi(page, "GET", "/notifications");
		expect(status).toBe(200);

		const serialized = JSON.stringify(body);
		expect(serialized).not.toContain("demo-token");
		expect(serialized).not.toContain("hooks.slack.com");
	});

	test("the rolled back application explains the rollback", async ({ page }) => {
		const { status, body } = await demoApi(page, "GET", `/applications/${MONITORING_APP.id}`);
		expect(status).toBe(200);
		expect(body.rolledBackAt).not.toBeNull();
		expect(body.rolledBackCommit).toBe("5c0a8e2f91d3476b0a8e1d4c6f32b975e1a0d843");
		expect(body.syncStatus).toBe("out_of_sync");
		expect(body.autoSyncPaused).toBe(true);

		const events = await demoApi(page, "GET", `/applications/${MONITORING_APP.id}/events`);
		expect(events.status).toBe(200);
		expect(events.body.items).toEqual(
			expect.arrayContaining([
				expect.objectContaining({ type: "rollback", source: "health_check", status: "succeeded" }),
				expect.objectContaining({ type: "deployment", status: "failed" }),
			]),
		);

		await page.goto(`/applications/${MONITORING_APP.id}/history`);
		await expect(page.getByText("Rolled back — out of sync with Git")).toBeVisible();
		await expect(page.getByText("Auto-sync is paused until you resume it.")).toBeVisible();
	});

	test("the healthy application has no rollback banner", async ({ page }) => {
		await page.goto(`/applications/${WEB_APP.id}/history`);

		await expect(page.getByRole("heading", { name: "History" })).toBeVisible();
		await expect(page.getByText("Rolled back — out of sync with Git")).toHaveCount(0);
	});

	test("environment files come from the demo data and secrets stay hidden", async ({ page }) => {
		const { status, body } = await demoApi(page, "GET", `/applications/${WEB_APP.id}/env`);
		expect(status).toBe(200);

		expect(body.files).toHaveLength(1);
		const [file] = body.files;
		expect(file.path).toBe(".env");
		expect(file.entries).toEqual(
			expect.arrayContaining([
				{ key: "APP_ENV", isSecret: false, value: "production", isSet: true },
				{ key: "DATABASE_PASSWORD", isSecret: true, isSet: true },
			]),
		);
		for (const entry of file.entries.filter((entry: { isSecret: boolean }) => entry.isSecret)) {
			expect(entry).not.toHaveProperty("value");
		}

		const monitoring = await demoApi(page, "GET", `/applications/${MONITORING_APP.id}/env`);
		expect(monitoring.status).toBe(200);
		expect(monitoring.body.files.map((file: { path: string }) => file.path)).toEqual([
			"prometheus.env",
			"grafana.env",
		]);

		const unknown = await demoApi(page, "GET", "/applications/does-not-exist/env");
		expect(unknown.status).toBe(404);
	});

	test("the system information hides the host configuration", async ({ page }) => {
		const { status, body } = await demoApi(page, "GET", "/admin/system-info");
		expect(status).toBe(200);

		expect(body.host).toBe(REDACTED);
		expect(body.port).toBe(REDACTED);
		expect(body.trustedProxies).toEqual([REDACTED]);
		expect(body.allowedIps).toEqual([REDACTED]);
	});

	test("changes are rejected and leave the data untouched", async ({ page }) => {
		const writes: [method: string, path: string, body?: unknown][] = [
			["POST", "/repositories", { url: "https://github.com/x/y", provider: "github" }],
			["PUT", "/repositories/demo-repository", { name: "Renamed" }],
			["DELETE", "/repositories/demo-repository"],
			["POST", "/agents", { name: "New Agent" }],
			["PUT", "/agents/demo-agent", { name: "Renamed" }],
			["POST", "/agents/demo-agent/rotate-token"],
			["DELETE", "/agents/demo-agent"],
			["POST", "/applications", { name: "New App" }],
			["PUT", `/applications/${WEB_APP.id}`, { name: "Renamed" }],
			["DELETE", `/applications/${WEB_APP.id}`],
			["POST", `/applications/${WEB_APP.id}/deploy`],
			["POST", `/applications/${WEB_APP.id}/pull-images`],
			["POST", `/applications/${WEB_APP.id}/deployments/demo-deployment-web-image/rollback`],
			["POST", `/applications/${MONITORING_APP.id}/resume-auto-sync`],
			["PUT", `/applications/${WEB_APP.id}/rollback`, { enabled: false }],
			["PUT", `/applications/${WEB_APP.id}/env`, { files: [] }],
			["POST", `/applications/${WEB_APP.id}/image-webhook`],
			["DELETE", `/applications/${WEB_APP.id}/image-webhook`],
			["POST", "/notifications", { name: "New", type: "slack" }],
			["DELETE", "/notifications/demo-notification-slack"],
			["POST", "/notifications/demo-notification-slack/test"],
			["POST", "/admin/users", { name: "New", email: "new@example.com", roleIds: [] }],
			["PUT", "/admin/users/demo-member", { roleIds: [] }],
			["DELETE", "/admin/users/demo-member"],
			["POST", "/admin/oidc-providers", { name: "SSO" }],
			["PUT", "/auth/profile", { name: "Changed" }],
			["POST", "/auth/change-password", { currentPassword: DEMO_PASSWORD, newPassword: "x" }],
			["POST", "/auth/logout-all"],
		];

		for (const [method, path, body] of writes) {
			const { status, body: response } = await demoApi(page, method, path, body);
			expect({ status, response }, `${method} ${path}`).toEqual({
				status: 403,
				response: BLOCKED_MESSAGE,
			});
		}

		expect(await names(page, "/applications")).toHaveLength(3);
		expect(await names(page, "/repositories")).toHaveLength(2);
		expect(await names(page, "/notifications")).toHaveLength(2);
		const profile = await demoApi(page, "GET", "/auth/profile");
		expect(profile.body.name).toBe("Demo User");
	});

	test("blocked actions in the UI explain that they are not allowed", async ({ page }) => {
		await page.goto("/admin/users");
		await page.getByRole("button", { name: "Add User" }).click();

		const dialog = page.getByRole("dialog", { name: "Add User" });
		await dialog.getByLabel("Name", { exact: true }).fill("Blocked User");
		await dialog.getByLabel("Email", { exact: true }).fill("blocked@example.com");
		await dialog.getByRole("button", { name: "Create User" }).click();

		await expectToast(page, "Not allowed in demo mode.");
		await expect(dialog).toBeVisible();
		expect(await names(page, "/admin/users")).toHaveLength(2);
	});

	test("there is no live event stream and agents cannot connect", async ({ page }) => {
		for (const path of ["/events", "/ws"]) {
			const { status } = await demoApi(page, "GET", path);
			expect(status, path).toBe(404);
		}
	});

	test("signing out is allowed", async ({ page }) => {
		await page.goto("/");
		await signOutUi(page);

		const { status } = await demoApi(page, "GET", "/auth/profile");
		expect(status).toBe(401);
	});
});
