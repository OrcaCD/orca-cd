import { test, expect, type Page } from "@playwright/test";

async function openActivity(page: Page, days = 1) {
	// The overview shows onboarding instead of activity when there are no applications.
	await page.route("**/api/v1/applications", (route) =>
		route.fulfill({
			json: [
				{
					id: "tooltip-app",
					name: "Tooltip app",
					icon: "",
					syncStatus: "synced",
					healthStatus: "healthy",
					repositoryName: "Test",
					agentId: "test-agent",
					agentName: "Test agent",
					branch: "main",
					commit: "abc1234",
					path: "compose.yaml",
				},
			],
		}),
	);
	await page.route("**/api/v1/activity?*", (route) =>
		route.fulfill({
			json: {
				days: Array.from({ length: days }, (_, index) => ({
					date: `2026-10-${29 - index}`,
					succeeded: 3,
					failed: 1,
				})),
				recentDeployments: [],
			},
		}),
	);
	await page.goto("/");
	const card = page
		.locator('[data-slot="card"]')
		.filter({ has: page.getByText("Deployments", { exact: true }) });
	return card.locator('[data-slot="tooltip-trigger"], [aria-haspopup="dialog"]').first();
}

test("information tooltips open on desktop hover and keyboard focus", async ({ page }) => {
	const trigger = await openActivity(page);
	const tooltip = page.locator('[data-slot="tooltip-content"]');
	await trigger.hover();
	await expect(tooltip).toContainText("Succeeded: 3 · Failed: 1");
	await page.mouse.move(0, 0);
	await expect(tooltip).toBeHidden();
	await trigger.focus();
	await page.keyboard.press("Shift+Tab");
	await page.keyboard.press("Tab");
	await expect(tooltip).toBeVisible();
	await page.keyboard.press("Escape");
	await expect(tooltip).toBeHidden();
});

test.describe("hybrid mouse and touch input", () => {
	test.use({ hasTouch: true, isMobile: false, viewport: { width: 1280, height: 900 } });

	test.beforeEach(async ({ page }) => {
		// Playwright's touch emulation normally makes touch the primary input.
		// Model a Windows touchscreen laptop that reports a primary mouse instead.
		await page.addInitScript(() => {
			const matchMedia = window.matchMedia.bind(window);
			const capabilities: Record<string, boolean> = {
				"(hover: none), (pointer: coarse)": false,
				"(hover: none)": false,
				"(pointer: coarse)": false,
				"(hover: hover)": true,
				"(pointer: fine)": true,
				"(any-pointer: coarse)": true,
			};
			window.matchMedia = (query) => {
				const result = matchMedia(query);
				if (query in capabilities) {
					Object.defineProperty(result, "matches", { value: capabilities[query] });
				}
				return result;
			};
		});
	});

	test("hover, touch, and hover again work on the same trigger", async ({ page }) => {
		const trigger = await openActivity(page, 7);
		await expect
			.poll(() =>
				page.evaluate(() => ({
					hover: matchMedia("(hover: hover)").matches,
					fine: matchMedia("(pointer: fine)").matches,
					touch: matchMedia("(any-pointer: coarse)").matches,
				})),
			)
			.toEqual({ hover: true, fine: true, touch: true });
		const tooltip = page.locator('[data-slot="tooltip-content"]');
		const popup = page.getByRole("dialog", { name: "Details" });
		await trigger.hover();
		await expect(tooltip).toContainText("Succeeded: 3 · Failed: 1");
		await trigger.tap();
		await expect(popup).toContainText("Succeeded: 3 · Failed: 1");
		await expect(tooltip).toBeHidden();
		await trigger.tap();
		await expect(popup).toBeHidden();
		await trigger.tap();
		await expect(popup).toBeVisible();
		await page.getByRole("heading", { name: "Overview" }).tap();
		await expect(popup).toBeHidden();
		await page.mouse.move(0, 0);
		await trigger.hover();
		await expect(tooltip).toBeVisible();
	});
	test("form help buttons support both hover and touch", async ({ page }) => {
		await page.route("**/api/v1/repositories/info-repo", (route) =>
			route.fulfill({
				json: {
					id: "info-repo",
					name: "Info repository",
					commitSigningSupported: true,
					requireSignedCommits: false,
					commitSigningKeys: [],
				},
			}),
		);
		await page.goto("/repositories/info-repo/settings/signing");
		const trigger = page.getByRole("button", { name: "Details", exact: true });
		await trigger.hover();
		await expect(page.locator('[data-slot="tooltip-content"]')).toHaveText(
			"Reject commits that are not signed by a trusted key.",
		);
		await trigger.tap();
		await expect(page.getByRole("dialog", { name: "Details" })).toHaveText(
			"Reject commits that are not signed by a trusted key.",
		);
		await expect(page.locator('[data-slot="tooltip-content"]')).toBeHidden();
		await trigger.tap();
		await expect(page.getByRole("dialog", { name: "Details" })).toBeHidden();
		await expect(page.getByRole("switch")).not.toBeChecked();
	});
});

test.describe("touch information popovers", () => {
	test.use({ hasTouch: true, isMobile: true, viewport: { width: 390, height: 844 } });

	test("tap toggles details and tapping outside dismisses them", async ({ page }) => {
		const trigger = await openActivity(page);
		const popup = page.getByRole("dialog", { name: "Details" });
		await trigger.tap();
		await expect(popup).toContainText("Succeeded: 3 · Failed: 1");
		await trigger.tap();
		await expect(popup).toBeHidden();
		await trigger.tap();
		await expect(popup).toBeVisible();
		await page.getByRole("heading", { name: "Overview" }).tap();
		await expect(popup).toBeHidden();
	});

	test("large touch screens also use tap and support Escape", async ({ page }) => {
		await page.setViewportSize({ width: 1280, height: 900 });
		const trigger = await openActivity(page);
		const popup = page.getByRole("dialog", { name: "Details" });
		await trigger.tap();
		await expect(popup).toBeVisible();
		await page.keyboard.press("Escape");
		await expect(popup).toBeHidden();
	});
});
