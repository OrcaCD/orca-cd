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

test("information tooltips share one media query and release its listener", async ({ page }) => {
	await page.addInitScript(() => {
		const stats = { queries: 0, listeners: 0 };
		Object.assign(window, { tooltipMediaStats: stats });
		const matchMedia = window.matchMedia.bind(window);
		window.matchMedia = (query) => {
			const result = matchMedia(query);
			if (query === "(hover: none), (pointer: coarse)") {
				stats.queries++;
				const add = result.addEventListener.bind(result);
				const remove = result.removeEventListener.bind(result);
				result.addEventListener = (...args: Parameters<MediaQueryList["addEventListener"]>) => {
					if (args[0] === "change") {
						stats.listeners++;
					}
					add(...args);
				};
				result.removeEventListener = (
					...args: Parameters<MediaQueryList["removeEventListener"]>
				) => {
					if (args[0] === "change") {
						stats.listeners--;
					}
					remove(...args);
				};
			}
			return result;
		};
	});
	const trigger = await openActivity(page, 7);
	await trigger.hover();
	await expect(page.locator('[data-slot="tooltip-content"]')).toBeVisible();
	const stats = () => page.evaluate(() => Reflect.get(window, "tooltipMediaStats"));
	await expect.poll(stats).toEqual({ queries: 1, listeners: 1 });
	await page.getByRole("link", { name: "Notifications", exact: true }).click();
	await expect.poll(stats).toEqual({ queries: 1, listeners: 0 });
	await page.goBack();
	await trigger.hover();
	await expect(page.locator('[data-slot="tooltip-content"]')).toBeVisible();
	await expect.poll(stats).toEqual({ queries: 1, listeners: 1 });
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
