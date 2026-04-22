import { test, expect } from "@playwright/test";

test("Dashboard", async ({ page }) => {
	await page.goto("/");

	await expect(page.getByRole("heading", { name: "Applications" })).toBeVisible();
	await expect(page.getByText("No applications found")).toBeVisible();

	// ...
});

// Connect Repository, Create Application, ...
