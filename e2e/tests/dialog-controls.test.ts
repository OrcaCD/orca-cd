import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => {
	await page.route("**/api/v1/events", (route) => route.abort());
	await page.route("**/api/v1/auth/profile", (route) =>
		route.fulfill({
			json: {
				id: "dialog-user",
				name: "Dialog user",
				email: "dialogs@example.com",
				isAdmin: true,
				roles: [],
				permissions: {},
				isLocal: true,
				passwordChangeRequired: false,
			},
		}),
	);
	await page.route(/\/api\/v1\/(agents|applications|repositories|notifications)$/, (route) =>
		route.fulfill({ json: [] }),
	);
});

test("repository step labels remain accessible without visible captions", async ({ page }) => {
	await page.goto("/repositories");
	await page.getByRole("button", { name: "Add Repository" }).first().click();
	const dialog = page.getByRole("dialog");
	for (const label of ["Provider", "Repository", "Sync", "Review"]) {
		await expect(dialog.getByRole("tab", { name: label, exact: true })).toBeVisible();
		await expect(dialog.getByText(label, { exact: true })).toHaveCount(0);
	}
});

test("icon picker shows all icons after selection and after dismissing a search", async ({
	page,
}) => {
	await page.goto("/agents");
	await page.getByRole("button", { name: "Add Agent" }).first().click();
	const input = page.getByRole("combobox", { name: "Select an icon" });
	await input.click();
	await expect(page.getByRole("option")).toHaveCount(15);
	await page.getByRole("option", { name: "Cloud", exact: true }).click();
	await expect(input).toHaveValue("Cloud");
	await input.click();
	await expect(page.getByRole("option")).toHaveCount(15);
	await input.fill("rocket");
	await expect(page.getByRole("option", { name: "Rocket", exact: true })).toBeVisible();
	await expect(page.getByRole("option", { name: "Database", exact: true })).toHaveCount(0);
	await input.press("Escape");
	await input.click();
	await expect(page.getByRole("option")).toHaveCount(15);
	await page.getByRole("option", { name: "Rocket", exact: true }).click();
	await expect(input).toHaveValue("Rocket");
});

for (const { path, button, next } of [
	{ path: "/applications", button: "New Application", next: false },
	{ path: "/notifications", button: "Add Notification", next: true },
]) {
	test(`${path} dialog leaves room for input focus rings in its scroll area`, async ({ page }) => {
		await page.goto(path);
		await page.getByRole("button", { name: button }).first().click();
		const dialog = page.getByRole("dialog");
		if (next) {
			await dialog.getByRole("button", { name: "Next", exact: true }).click();
		}
		const input = dialog.getByRole("textbox", { name: "Name", exact: true });
		await expect(input).toBeVisible();
		const clearance = await input.evaluate((element) => {
			const inputRect = element.getBoundingClientRect();
			const form = element.closest("form")!;
			const formRect = form.getBoundingClientRect();
			return {
				left: inputRect.left - formRect.left,
				right: formRect.left + form.clientWidth - inputRect.right,
			};
		});
		expect(clearance.left).toBeGreaterThanOrEqual(3);
		expect(clearance.right).toBeGreaterThanOrEqual(3);
	});
}
