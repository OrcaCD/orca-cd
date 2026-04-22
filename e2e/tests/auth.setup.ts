import { join } from "node:path";
import { test as setup } from "@playwright/test";

const authFile = join(import.meta.dirname, "../playwright/.auth/user.json");

setup("Authenticate", async ({ page }) => {
	await page.goto("/login");

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
