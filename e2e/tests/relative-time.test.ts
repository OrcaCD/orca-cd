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

test("table dates are relative and missing or invalid dates stay readable", async ({ page }) => {
	await page.clock.setFixedTime(now);
	await page.addInitScript(() => localStorage.setItem("orca-preferred-layout", "table"));
	await page.route("**/api/v1/applications", (route) =>
		route.fulfill({
			json: [
				app,
				{ ...app, id: "never", name: "Never synced", lastSyncedAt: null },
				{ ...app, id: "invalid", name: "Invalid date", lastSyncedAt: "invalid" },
			],
		}),
	);
	await page.goto("/applications");
	await expect(
		page.getByRole("row").filter({ hasText: "Relative time app" }).locator("time"),
	).toHaveText("4 days ago");
	await expect(page.getByRole("row").filter({ hasText: "Never synced" })).toContainText("Never");
	await expect(page.getByRole("row").filter({ hasText: "Invalid date" })).toContainText("Never");
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
});
