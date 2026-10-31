import { test, expect, type Page } from "@playwright/test";

const now = new Date("2026-10-29T12:34:56Z");
const timestamp = "2026-10-25T12:34:56Z";
const app = {
	id: "relative-time-app",
	name: "Relative time app",
	icon: "",
	syncStatus: "synced",
	healthStatus: "healthy",
	repositoryName: "Test repository",
	agentId: "test-agent",
	agentName: "Test agent",
	branch: "main",
	commit: "abc1234",
	path: "compose.yaml",
	lastSyncedAt: timestamp,
};

async function mockApplications(page: Page) {
	await page.route("**/api/v1/applications", (route) => route.fulfill({ json: [app] }));
}

async function exactDate(page: Page, locale = "en") {
	return page.evaluate(({ timestamp, locale }) => new Date(timestamp).toLocaleString(locale), {
		timestamp,
		locale,
	});
}

test("relative dates expose exact timestamps on hover and keyboard focus", async ({ page }) => {
	await page.clock.setFixedTime(now);
	await mockApplications(page);
	await page.goto("/applications");
	const time = page.locator("time");
	await expect(time).toHaveText("4 days ago");
	await expect(time).toHaveAttribute("datetime", new Date(timestamp).toISOString());
	await time.hover();
	const tooltip = page.locator('[data-slot="tooltip-content"]');
	await expect(tooltip).toHaveText(await exactDate(page));
	await page.mouse.move(0, 0);
	await expect(tooltip).toBeHidden();
	await time.focus();
	await page.keyboard.press("Shift+Tab");
	await page.keyboard.press("Tab");
	await expect(tooltip).toBeVisible();
	await page.keyboard.press("Escape");
	await expect(tooltip).toBeHidden();
});

test("relative dates refresh while the page stays open", async ({ page }) => {
	await page.clock.install({ time: now });
	await page.route("**/api/v1/applications", (route) =>
		route.fulfill({
			json: [{ ...app, lastSyncedAt: new Date(now.getTime() - 120_000).toISOString() }],
		}),
	);
	await page.goto("/applications");
	await expect(page.locator("time")).toHaveText("2 minutes ago");
	await page.clock.fastForward(60_000);
	await expect(page.locator("time")).toHaveText("3 minutes ago");
});

test("fresh events never appear to be in the future", async ({ page }) => {
	await page.clock.setFixedTime(now);
	await page.route("**/api/v1/applications", (route) =>
		route.fulfill({
			json: [{ ...app, lastSyncedAt: new Date(now.getTime() + 30_000).toISOString() }],
		}),
	);
	await page.goto("/applications");
	await expect(page.locator("time")).toHaveText("now");
});

test("relative dates refresh immediately when a background tab becomes visible", async ({
	page,
}) => {
	await page.clock.install({ time: now });
	await page.route("**/api/v1/applications", (route) =>
		route.fulfill({
			json: [{ ...app, lastSyncedAt: new Date(now.getTime() - 120_000).toISOString() }],
		}),
	);
	await page.goto("/applications");
	await expect(page.locator("time")).toHaveText("2 minutes ago");
	await page.clock.setSystemTime(new Date(now.getTime() + 300_000));
	await page.evaluate(() => {
		Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
		document.dispatchEvent(new Event("visibilitychange"));
	});
	await expect(page.locator("time")).toHaveText("2 minutes ago");
	await page.evaluate(() => {
		Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
		document.dispatchEvent(new Event("visibilitychange"));
	});
	await expect(page.locator("time")).toHaveText("7 minutes ago");
});

test("table dates are relative and missing or invalid dates stay readable", async ({ page }) => {
	await page.clock.setFixedTime(now);
	await page.addInitScript(() => localStorage.setItem("orca-preferred-layout", "table"));
	await page.route("**/api/v1/applications", (route) =>
		route.fulfill({
			json: [
				app,
				{ ...app, id: "never", name: "Never synced", lastSyncedAt: null },
				{ ...app, id: "invalid", name: "Invalid date", lastSyncedAt: "invalid" },
				{ ...app, id: "zero", name: "Numeric date", lastSyncedAt: "0" },
				{ ...app, id: "one", name: "Ambiguous date", lastSyncedAt: "1" },
				{ ...app, id: "impossible", name: "Impossible date", lastSyncedAt: "2026-02-30T12:00:00Z" },
				{ ...app, id: "blank", name: "Blank date", lastSyncedAt: " " },
				{ ...app, id: "offset", name: "Offset date", lastSyncedAt: "2026-10-25T14:34:56+02:00" },
			],
		}),
	);
	await page.goto("/applications");
	await expect(
		page.getByRole("row").filter({ hasText: "Relative time app" }).locator("time"),
	).toHaveText("4 days ago");
	await expect(page.getByRole("row").filter({ hasText: "Never synced" })).toContainText("Never");
	await expect(page.getByRole("row").filter({ hasText: "Invalid date" })).toContainText("Never");
	await expect(page.getByRole("row").filter({ hasText: "Numeric date" })).toContainText("Never");
	await expect(page.getByRole("row").filter({ hasText: "Ambiguous date" })).toContainText("Never");
	await expect(page.getByRole("row").filter({ hasText: "Impossible date" })).toContainText("Never");
	await expect(page.getByRole("row").filter({ hasText: "Blank date" })).toContainText("Never");
	await expect(
		page.getByRole("row").filter({ hasText: "Offset date" }).locator("time"),
	).toHaveAttribute("datetime", new Date(timestamp).toISOString());
	await expect(page.locator("time")).toHaveCount(2);
});

test("relative and exact dates follow the selected language", async ({ page }) => {
	await page.clock.setFixedTime(now);
	await page.addInitScript(() => localStorage.setItem("orca-locale", "de"));
	await mockApplications(page);
	await page.goto("/applications");
	const time = page.locator("time");
	await expect(time).toHaveText("vor 4 Tagen");
	await time.hover();
	await expect(page.locator('[data-slot="tooltip-content"]')).toHaveText(
		await exactDate(page, "de"),
	);
});

test.describe("precise event timestamps", () => {
	const dates = [timestamp, "2026-10-25T12:36:56Z"];
	const events = dates.map((createdAt, index) => ({
		id: `event-${index}`,
		createdAt,
		completedAt: createdAt,
		type: "deployment",
		status: "succeeded",
		source: "manual",
		actorName: null,
		commitHash: app.commit,
		commitMessage: "Test deployment",
		errorMessage: null,
	}));

	test.beforeEach(async ({ page }) => {
		await page.clock.setFixedTime(now);
		await page.route(`**/api/v1/applications/${app.id}`, (route) => route.fulfill({ json: app }));
		await page.route(`**/api/v1/applications/${app.id}/events?*`, (route) =>
			route.fulfill({
				json: { items: events, hasMore: false },
			}),
		);
	});

	test("history tables show full dates and distinguish events on the same day", async ({
		page,
	}) => {
		await page.addInitScript(() => localStorage.setItem("orca-history-layout", "table"));
		await page.goto(`/applications/${app.id}/history`);
		const expected = await page.evaluate(
			(dates) => dates.map((date) => new Date(date).toLocaleString("en")),
			dates,
		);
		await expect(page.locator("tbody time")).toHaveText(expected);
	});

	test("grouped history shows exact times with full dates on hover", async ({ page }) => {
		await page.addInitScript(() => localStorage.setItem("orca-history-layout", "timeline"));
		await page.goto(`/applications/${app.id}/history`);
		const expected = await page.evaluate((dates) => {
			const formatter = new Intl.DateTimeFormat("en", { timeStyle: "short" });
			return dates.map((date) => formatter.format(new Date(date)));
		}, dates);
		const times = page.locator("ol time");
		await expect(times).toHaveText(expected);
		await times.first().hover();
		await expect(page.locator('[data-slot="tooltip-content"]')).toHaveText(await exactDate(page));
	});

	test("audit logs show full dates without requiring hover", async ({ page }) => {
		await page.route("**/api/v1/admin/audit-logs?*", (route) =>
			route.fulfill({
				json: {
					items: dates.map((createdAt, index) => ({
						id: `audit-${index}`,
						createdAt,
						updatedAt: createdAt,
						userId: "review-user",
						user: { id: "review-user", name: "Review user", email: "review@example.com" },
						eventType: "created",
						targetType: "application",
						targetId: app.id,
					})),
					hasMore: false,
				},
			}),
		);
		await page.goto("/admin/audit-log");
		const expected = await page.evaluate(
			(dates) => dates.map((date) => new Date(date).toLocaleString("en")),
			dates,
		);
		await expect(page.locator("tbody time")).toHaveText(expected);
	});
});

test.describe("touch dates in linked cards", () => {
	test.use({ hasTouch: true, isMobile: true, viewport: { width: 390, height: 844 } });

	test("tap opens the date without navigating and the card still navigates", async ({ page }) => {
		await page.clock.setFixedTime(now);
		await mockApplications(page);
		await page.goto("/applications");
		const time = page.locator("time");
		await time.tap();
		const popup = page.getByRole("dialog", { name: "Details" });
		await expect(popup).toHaveText(await exactDate(page));
		await expect(page).toHaveURL(/\/applications\/?$/);
		await time.tap();
		await expect(popup).toBeHidden();
		await page.getByRole("link", { name: app.name, exact: true }).tap();
		await expect(page).toHaveURL(new RegExp(`/applications/${app.id}`));
	});

	test("recent deployment rows navigate from their padding while timestamps remain independent", async ({
		page,
	}) => {
		await page.clock.setFixedTime(now);
		await mockApplications(page);
		await page.route("**/api/v1/activity?*", (route) =>
			route.fulfill({
				json: {
					days: [],
					recentDeployments: [
						{
							id: "recent-deployment",
							applicationId: app.id,
							applicationName: app.name,
							applicationIcon: "",
							type: "deployment",
							status: "succeeded",
							source: "manual",
							createdAt: timestamp,
							completedAt: timestamp,
							actorName: null,
							commitHash: app.commit,
							commitMessage: "A recent deployment",
							errorMessage: null,
						},
					],
				},
			}),
		);
		await page.goto("/");
		const row = page.locator('[data-slot="item"]').filter({ hasText: app.name });
		const time = row.locator("time");
		await time.tap();
		await expect(page.getByRole("dialog", { name: "Details" })).toHaveText(await exactDate(page));
		await expect(page).toHaveURL("/");
		await time.tap();
		await expect(page.getByRole("dialog", { name: "Details" })).toBeHidden();
		const bounds = await row.boundingBox();
		expect(bounds).not.toBeNull();
		await row.tap({ position: { x: bounds!.width / 2, y: bounds!.height - 4 } });
		await expect(page).toHaveURL(new RegExp(`/applications/${app.id}/history`));
	});
});
