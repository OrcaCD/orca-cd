import { test, expect, type Page } from "@playwright/test";

async function openActivity(page: Page) {
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
				days: [{ date: "2026-10-29", succeeded: 3, failed: 1 }],
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
