import { test, expect } from "@playwright/test";

// Run all tests in this file without authentication
test.use({ storageState: { cookies: [], origins: [] } });

test("Login", async ({ page }) => {
	await page.goto("/");

	await expect(page).toHaveTitle(/Login/);

	await page.getByRole("textbox", { name: "email" }).fill("testadmin@example.com");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("wrongpassword");
	await page.locator("form button[type='submit']").click();

	await expect(page.getByText("invalid email or password")).toBeVisible();

	await page.getByRole("textbox", { name: "email" }).fill("does-not-exist@example.com");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("TestPassword123!");
	await page.locator("form button[type='submit']").click();

	await expect(page.getByText("invalid email or password")).toBeVisible();

	await page.getByRole("textbox", { name: "email" }).fill("testadmin@example.com");
	await page.locator("form button[type='submit']").click();

	await page.waitForURL("/");
});
