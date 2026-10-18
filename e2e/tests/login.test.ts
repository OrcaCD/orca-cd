import { test, expect } from "@playwright/test";
import { ADMIN_EMAIL, ADMIN_PASSWORD } from "./const.ts";

// Run all tests in this file without authentication
test.use({ storageState: { cookies: [], origins: [] } });

test("Login", async ({ page }) => {
	await page.goto("/");

	await expect(page).toHaveTitle(/Login/);

	await page.getByRole("textbox", { name: "email" }).fill(ADMIN_EMAIL);
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("wrongpassword");
	await page.locator("form button[type='submit']").click();

	await expect(page.getByText("invalid email or password")).toBeVisible();

	await page.getByRole("textbox", { name: "email" }).fill("does-not-exist@example.com");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill(ADMIN_PASSWORD);
	await page.locator("form button[type='submit']").click();

	await expect(page.getByText("invalid email or password")).toBeVisible();

	await page.getByRole("textbox", { name: "email" }).fill(ADMIN_EMAIL);
	await page.locator("form button[type='submit']").click();

	await page.waitForURL("/");
});

test("Register page redirects to login after the initial setup", async ({ page }) => {
	await page.goto("/register");

	await page.waitForURL("/login");
	await expect(page).toHaveTitle(/Login/);
});
