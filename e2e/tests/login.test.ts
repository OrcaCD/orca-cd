import { test, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { importCredential } from "./helpers/passkeys.ts";

test.use({ storageState: { cookies: [], origins: [] } });

test("Login with a discoverable passkey", async ({ page }) => {
	const credential = JSON.parse(
		await readFile(join(import.meta.dirname, "../playwright/.auth/passkey.json"), "utf8"),
	);
	await importCredential(page, credential);
	await page.goto("/");
	await expect(page).toHaveTitle(/Login/);
	await expect(page.locator("input[type=password]")).toHaveCount(0);
	await page.getByRole("button", { name: "Sign in with a passkey" }).click();
	await page.waitForURL("/");
});

test("Register page offers invitation enrollment after initial setup", async ({ page }) => {
	await page.goto("/register");
	await expect(page.getByLabel("Enrollment code", { exact: true })).toBeVisible();
	await page.getByLabel("Enrollment code", { exact: true }).fill("invalid-code");
	await page.getByRole("button", { name: "Create passkey", exact: true }).click();
	await expect(page.getByText("invalid or expired enrollment code")).toBeVisible();
});
