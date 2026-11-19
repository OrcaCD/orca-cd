import { writeFile } from "node:fs/promises";
import { credentials, installAuthenticator } from "./helpers/passkeys.ts";
import { join } from "node:path";
import { expect, test as setup } from "@playwright/test";
import { ADMIN_EMAIL, ADMIN_NAME } from "./const.ts";
import { readSetupCode } from "./helpers/setup-code.ts";

const authFile = join(import.meta.dirname, "../playwright/.auth/user.json");

setup("Authenticate", async ({ page }) => {
	const setupCode = await readSetupCode();
	await installAuthenticator(page);

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

	await page.getByRole("textbox", { name: "name" }).fill(ADMIN_NAME);
	await page.getByRole("textbox", { name: "email" }).fill(ADMIN_EMAIL);

	await page.locator("form button[type='submit']").click();

	await page.waitForURL("/");

	await page.context().storageState({ path: authFile });
	await writeFile(
		join(import.meta.dirname, "../playwright/.auth/passkey.json"),
		JSON.stringify((await credentials(page))[0]),
	);
});
