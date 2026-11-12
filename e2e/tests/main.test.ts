import { test, expect } from "@playwright/test";

test("Dashboard", async ({ page }) => {
	await page.goto("/");

	await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
	await expect(page.getByText("Get started")).toBeVisible();

	await page.goto("/applications");
	await expect(page.getByRole("heading", { name: "Applications", exact: true })).toBeVisible();
	await expect(page.getByText("No applications yet")).toBeVisible();

	// ...
});

// Connect Repository, Create Application, ...
