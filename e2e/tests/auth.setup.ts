import { join } from "node:path";
import { expect, test as setup } from "@playwright/test";
import { readSetupCode } from "./helpers/setup-code.ts";

const authFile = join(import.meta.dirname, "../playwright/.auth/user.json");

setup("Authenticate", async ({ page }) => {
	const setupCode = await readSetupCode();

	await page.goto("/login");
	await page.waitForURL("/register");

	const setupCodeInput = page.getByRole("textbox", { name: "Setup code" });
	await setupCodeInput.fill("AAAA-BBBB-CCCC-DDDD");
	await page.locator("form button[type='submit']").click();
	await expect(page.getByText("invalid setup code")).toBeVisible();
	await expect(setupCodeInput).toBeVisible();

	await setupCodeInput.fill(setupCode);
	await page.locator("form button[type='submit']").click();
	await expect(page.getByRole("textbox", { name: "name" })).toBeVisible();

	// The link logged by the hub verifies the code without any input
	await page.goto("about:blank");
	await page.goto(`/register#setup-code=${setupCode}`);
	await expect(page.getByRole("textbox", { name: "name" })).toBeVisible();
	expect(new URL(page.url()).hash).toBe("");

	await page.getByRole("textbox", { name: "name" }).fill("Testadmin");
	await page.getByRole("textbox", { name: "email" }).fill("testadmin@example.com");

	await page.getByRole("textbox", { name: "Password", exact: true }).fill("TestPassword123!");
	await page
		.getByRole("textbox", { name: "Confirm Password", exact: true })
		.fill("TestPassword123!");

	await page.locator("form button[type='submit']").click();

	await page.waitForURL("/");

	await page.context().storageState({ path: authFile });
});
