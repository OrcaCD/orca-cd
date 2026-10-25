import { test, expect, type Locator, type Page } from "@playwright/test";
import { ADMIN_EMAIL, ADMIN_NAME } from "./const.ts";
import { api } from "./helpers/api.ts";
import { gotoApp } from "./helpers/navigation.ts";
import { listUsers, uniqueId } from "./helpers/oidc.ts";
import {
	NEW_PASSWORD,
	apiSignIn,
	cleanup,
	createActiveUser,
	createUser,
	expectToast,
	fillPasswords,
	openSession,
	signInUi,
	signOutUi,
	trackUser,
} from "./helpers/users.ts";

test.afterEach(async ({ page }) => {
	await cleanup(page);
});

async function openUsers(page: Page) {
	await gotoApp(page, "/admin/users");
	await expect(page.getByPlaceholder("Search users...")).toBeVisible();
}

function rowOf(page: Page, email: string) {
	return page.getByRole("row").filter({ hasText: email });
}

async function chooseAction(page: Page, email: string, action: "Edit" | "Delete") {
	const row = rowOf(page, email);
	await row.getByRole("button", { name: "Actions" }).click();
	await page
		.getByRole("menu", { name: "Actions" })
		.getByRole("button", { name: action, exact: true })
		.click();
	await expect(page.getByRole("menu", { name: "Actions" })).toBeHidden();
}

async function fillUserForm(
	dialog: Locator,
	values: { name?: string; email?: string; role?: "Admin" | "User" },
) {
	if (values.name !== undefined) {
		await dialog.getByLabel("Name", { exact: true }).fill(values.name);
	}
	if (values.email !== undefined) {
		await dialog.getByLabel("Email", { exact: true }).fill(values.email);
	}
	if (values.role !== undefined) {
		await dialog.getByRole("radio", { name: values.role, exact: true }).click();
	}
}

async function createUserInUi(page: Page, values: Parameters<typeof fillUserForm>[1]) {
	await page.getByRole("button", { name: "Add User" }).click();
	const dialog = page.getByRole("dialog", { name: "Add User" });
	await fillUserForm(dialog, values);
	await dialog.getByRole("button", { name: "Create User" }).click();
	return dialog;
}

async function findUser(page: Page, email: string) {
	return (await listUsers(page)).find((user) => user.email === email.toLowerCase());
}

async function confirmDelete(page: Page) {
	const dialog = page.getByRole("dialog", { name: "Delete user?" });
	await expect(dialog).toBeVisible();
	await dialog.getByRole("button", { name: "Proceed" }).click();
}

test.describe("Creating users", () => {
	test("a created user signs in and has to choose a new password", async ({ page, browser }) => {
		const id = uniqueId("create");
		const name = `Created ${id}`;
		const email = `${id}@example.com`;
		await openUsers(page);

		await createUserInUi(page, { name, email });

		await expectToast(page, "User created");
		const generated = page.getByRole("dialog", { name: "Generated Password" });
		const generatedPassword = await generated.locator("#generated-password").inputValue();
		expect(generatedPassword.length).toBeGreaterThanOrEqual(12);
		await generated.getByRole("button", { name: "Done" }).click();

		const row = rowOf(page, email);
		await expect(row).toBeVisible();
		await expect(row).toContainText(name);
		await expect(row.getByText("User", { exact: true })).toBeVisible();
		await expect(row.getByText("Password", { exact: true })).toBeVisible();
		const created = await findUser(page, email);
		expect(created).toMatchObject({ name, role: "user", providers: ["password"] });
		trackUser(created!.id);

		const { page: userPage } = await openSession(browser);
		await signInUi(userPage, email, generatedPassword);
		await userPage.waitForURL("/");
		const dialog = userPage.getByRole("dialog", { name: "Password Change Required" });
		await expect(dialog).toBeVisible();
		expect((await api(userPage, "GET", "/auth/profile")).body.passwordChangeRequired).toBe(true);

		await userPage.keyboard.press("Escape");
		await expect(dialog).toBeVisible();

		await fillPasswords(dialog, {
			current: generatedPassword,
			next: NEW_PASSWORD,
			confirm: "Different-Password-1!",
		});
		await dialog.getByRole("button", { name: "Update Password" }).click();
		await expect(dialog.getByText("Passwords do not match")).toBeVisible();

		await fillPasswords(dialog, {
			current: "Wrong-Password-123!",
			next: NEW_PASSWORD,
			confirm: NEW_PASSWORD,
		});
		await dialog.getByRole("button", { name: "Update Password" }).click();
		await expectToast(userPage, "current password is incorrect");
		await expect(dialog).toBeVisible();

		await fillPasswords(dialog, {
			current: generatedPassword,
			next: NEW_PASSWORD,
			confirm: NEW_PASSWORD,
		});
		await dialog.getByRole("button", { name: "Update Password" }).click();
		await expectToast(userPage, "Password updated");
		await expect(dialog).toHaveCount(0);
		await expect(userPage.getByRole("heading", { name: "Overview" })).toBeVisible();
		expect((await api(userPage, "GET", "/auth/profile")).body.passwordChangeRequired).toBe(false);

		await signOutUi(userPage);
		await signInUi(userPage, email, generatedPassword);
		await expect(userPage.getByText("invalid email or password")).toBeVisible();
		await signInUi(userPage, email, NEW_PASSWORD);
		await userPage.waitForURL("/");
		await expect(userPage.getByRole("heading", { name: "Overview" })).toBeVisible();
		await expect(dialog).toHaveCount(0);
	});

	test("the forced password change dialog offers to sign out", async ({ page, browser }) => {
		const user = await createUser(page);
		const { page: userPage } = await openSession(browser);

		await signInUi(userPage, user.email, user.password);
		await userPage.waitForURL("/");
		await userPage
			.getByRole("dialog", { name: "Password Change Required" })
			.getByRole("button", { name: "Sign Out" })
			.click();

		await userPage.waitForURL("/login");
		expect((await api(userPage, "GET", "/auth/profile")).status).toBe(401);
	});

	test("can create an admin", async ({ page }) => {
		const id = uniqueId("create-admin");
		const email = `${id}@example.com`;
		await openUsers(page);

		await createUserInUi(page, { name: `Admin ${id}`, email, role: "Admin" });

		await expectToast(page, "User created");
		await page
			.getByRole("dialog", { name: "Generated Password" })
			.getByRole("button", { name: "Done" })
			.click();
		await expect(rowOf(page, email).getByText("Admin", { exact: true })).toBeVisible();
		const created = await findUser(page, email);
		expect(created?.role).toBe("admin");
		trackUser(created!.id);
	});

	test("validates the form before sending it", async ({ page }) => {
		await openUsers(page);
		const requests: string[] = [];
		page.on("request", (request) => {
			if (
				request.method() === "POST" &&
				new URL(request.url()).pathname === "/api/v1/admin/users"
			) {
				requests.push(request.url());
			}
		});

		const dialog = await createUserInUi(page, { name: "ab", email: "user@localhost" });

		await expect(dialog.getByText("Name must be at least 3 characters")).toBeVisible();
		await expect(dialog.getByText("Must be a valid email address")).toBeVisible();
		expect(requests).toEqual([]);

		await dialog.getByRole("button", { name: "Cancel" }).click();
		await expect(dialog).toHaveCount(0);
	});

	test("rejects an email that is already used", async ({ page }) => {
		const existing = await createUser(page);
		await openUsers(page);

		for (const email of [existing.email, existing.email.toUpperCase(), ADMIN_EMAIL]) {
			const dialog = await createUserInUi(page, { name: "Duplicate User", email });

			await expectToast(page, "a user with this email already exists");
			await expect(dialog).toBeVisible();
			await dialog.getByRole("button", { name: "Cancel" }).click();
			await expect(dialog).toHaveCount(0);
		}

		const emails = (await listUsers(page)).map((user) => user.email);
		expect(emails.filter((email) => email === existing.email)).toHaveLength(1);
		expect(emails.filter((email) => email === ADMIN_EMAIL)).toHaveLength(1);
	});

	test("rejects an email that is already used when editing", async ({ page }) => {
		const first = await createUser(page);
		const second = await createUser(page);
		await openUsers(page);

		await chooseAction(page, second.email, "Edit");
		const dialog = page.getByRole("dialog", { name: "Edit User" });
		await fillUserForm(dialog, { email: first.email });
		await dialog.getByRole("button", { name: "Update User" }).click();

		await expectToast(page, "a user with this email already exists");
		await expect(dialog).toBeVisible();
		expect((await findUser(page, second.email))?.id).toBe(second.id);
	});
});

test.describe("Editing users", () => {
	test("changes name, email and role", async ({ page }) => {
		const user = await createUser(page);
		const id = uniqueId("edit");
		const name = `Edited ${id}`;
		const email = `edited-${id}@example.com`;
		await openUsers(page);

		await chooseAction(page, user.email, "Edit");
		const dialog = page.getByRole("dialog", { name: "Edit User" });
		await expect(dialog.getByLabel("Name", { exact: true })).toHaveValue(user.name);
		await expect(dialog.getByLabel("Email", { exact: true })).toHaveValue(user.email);
		await fillUserForm(dialog, { name, email, role: "Admin" });
		await dialog.getByRole("button", { name: "Update User" }).click();

		await expectToast(page, "User updated");
		await expect(dialog).toHaveCount(0);
		const row = rowOf(page, email);
		await expect(row).toContainText(name);
		await expect(row.getByText("Admin", { exact: true })).toBeVisible();
		await expect(rowOf(page, user.email)).toHaveCount(0);
		expect(await findUser(page, email)).toMatchObject({ id: user.id, name, role: "admin" });

		await chooseAction(page, email, "Edit");
		await fillUserForm(dialog, { role: "User" });
		await dialog.getByRole("button", { name: "Update User" }).click();

		await expectToast(page, "User updated");
		await expect(row.getByText("User", { exact: true })).toBeVisible();
		expect((await findUser(page, email))?.role).toBe("user");
	});

	test("cancelling keeps the user as it is", async ({ page }) => {
		const user = await createUser(page);
		await openUsers(page);

		await chooseAction(page, user.email, "Edit");
		const dialog = page.getByRole("dialog", { name: "Edit User" });
		await fillUserForm(dialog, { name: "Changed but cancelled" });
		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).toHaveCount(0);
		expect((await findUser(page, user.email))?.name).toBe(user.name);
	});

	test("validates the form before sending it", async ({ page }) => {
		const user = await createUser(page);
		await openUsers(page);
		let updates = 0;
		page.on("request", (request) => {
			if (request.method() === "PUT") {
				updates++;
			}
		});

		await chooseAction(page, user.email, "Edit");
		const dialog = page.getByRole("dialog", { name: "Edit User" });
		await fillUserForm(dialog, { name: "ab", email: "user@localhost" });
		await dialog.getByRole("button", { name: "Update User" }).click();

		await expect(dialog.getByText("Name must be at least 3 characters")).toBeVisible();
		await expect(dialog.getByText("Must be a valid email address")).toBeVisible();
		expect(updates).toBe(0);
	});

	test("resetting the password signs the user out and forces a new one", async ({
		page,
		browser,
	}) => {
		const user = await createActiveUser(page, browser);
		await openUsers(page);

		await chooseAction(page, user.email, "Edit");
		const dialog = page.getByRole("dialog", { name: "Edit User" });
		await dialog.getByRole("checkbox").check();
		await dialog.getByRole("button", { name: "Update User" }).click();

		await expectToast(page, "User updated and password reset");
		const generated = page.getByRole("dialog", { name: "Generated Password" });
		const generatedPassword = await generated.locator("#generated-password").inputValue();
		expect(generatedPassword).not.toBe(user.password);

		expect((await api(user.page, "GET", "/auth/profile")).status).toBe(401);

		const { page: freshPage } = await openSession(browser);
		const oldLogin = await api(freshPage, "POST", "/auth/login", {
			email: user.email,
			password: user.password,
		});
		expect(oldLogin.status).toBe(401);
		await apiSignIn(freshPage, user.email, generatedPassword);
		expect((await api(freshPage, "GET", "/auth/profile")).body.passwordChangeRequired).toBe(true);
	});

	test("the last admin can't be demoted", async ({ page }) => {
		const admins = (await listUsers(page)).filter((user) => user.role === "admin");
		expect(admins.map((admin) => admin.email)).toEqual([ADMIN_EMAIL]);
		await openUsers(page);

		await chooseAction(page, ADMIN_EMAIL, "Edit");
		const dialog = page.getByRole("dialog", { name: "Edit User" });
		await fillUserForm(dialog, { role: "User" });
		await dialog.getByRole("button", { name: "Update User" }).click();

		await expectToast(page, "cannot demote the last admin user");
		await expect(dialog).toBeVisible();
		expect((await findUser(page, ADMIN_EMAIL))?.role).toBe("admin");
	});

	test("an admin can be demoted while another admin exists", async ({ page }) => {
		const other = await createUser(page, { role: "admin" });
		await openUsers(page);

		await chooseAction(page, other.email, "Edit");
		const dialog = page.getByRole("dialog", { name: "Edit User" });
		await fillUserForm(dialog, { role: "User" });
		await dialog.getByRole("button", { name: "Update User" }).click();

		await expectToast(page, "User updated");
		expect((await findUser(page, other.email))?.role).toBe("user");
	});
});

test.describe("Deleting users", () => {
	test("deletes a user and revokes the session", async ({ page, browser }) => {
		const user = await createActiveUser(page, browser);
		await openUsers(page);

		await chooseAction(page, user.email, "Delete");
		const dialog = page.getByRole("dialog", { name: "Delete user?" });
		await expect(dialog).toContainText(user.name);
		await expect(dialog).toContainText(user.email);
		await dialog.getByRole("button", { name: "Cancel" }).click();
		await expect(dialog).toHaveCount(0);
		await expect(rowOf(page, user.email)).toBeVisible();
		expect(await findUser(page, user.email)).toBeDefined();

		await chooseAction(page, user.email, "Delete");
		await confirmDelete(page);

		await expectToast(page, "User deleted");
		await expect(rowOf(page, user.email)).toHaveCount(0);
		expect(await findUser(page, user.email)).toBeUndefined();
		expect((await api(page, "GET", `/admin/users/${user.id}`)).status).toBe(404);
		expect((await api(user.page, "GET", "/auth/profile")).status).toBe(401);
		const { page: freshPage } = await openSession(browser);
		const login = await api(freshPage, "POST", "/auth/login", {
			email: user.email,
			password: user.password,
		});
		expect(login.status).toBe(401);
	});

	test("an admin can be deleted while another admin exists", async ({ page }) => {
		const other = await createUser(page, { role: "admin" });
		await openUsers(page);

		await chooseAction(page, other.email, "Delete");
		await confirmDelete(page);

		await expectToast(page, "User deleted");
		await expect(rowOf(page, other.email)).toHaveCount(0);
		expect(await findUser(page, other.email)).toBeUndefined();
	});

	test("the last admin can't be deleted", async ({ page }) => {
		const admins = (await listUsers(page)).filter((user) => user.role === "admin");
		expect(admins.map((admin) => admin.email)).toEqual([ADMIN_EMAIL]);
		await openUsers(page);

		await chooseAction(page, ADMIN_EMAIL, "Delete");
		await confirmDelete(page);

		await expectToast(page, "cannot delete the last admin user");
		await expect(rowOf(page, ADMIN_EMAIL)).toBeVisible();
		expect(await findUser(page, ADMIN_EMAIL)).toBeDefined();
	});

	test("deleting an unknown user fails", async ({ page }) => {
		const response = await api(page, "DELETE", "/admin/users/does-not-exist");

		expect(response.status).toBe(404);
		expect(response.body).toEqual({ error: "user not found" });
	});
});

test.describe("User list", () => {
	test("marks the own account and shows all columns", async ({ page }) => {
		await openUsers(page);

		for (const column of ["Name", "Email", "Role", "Providers"]) {
			await expect(page.getByRole("columnheader", { name: column })).toBeVisible();
		}
		const row = rowOf(page, ADMIN_EMAIL);
		await expect(row).toContainText(ADMIN_NAME);
		await expect(row.getByText("You", { exact: true })).toBeVisible();
		await expect(row.getByText("Admin", { exact: true })).toBeVisible();
		await expect(row.getByText("Password", { exact: true })).toBeVisible();
	});

	test("search filters by name, email and role", async ({ page }) => {
		const tag = uniqueId("search");
		const alpha = await createUser(page, {
			name: `Alpha ${tag}`,
			email: `alpha-${tag}@example.com`,
		});
		const bravo = await createUser(page, {
			name: `Bravo ${tag}`,
			email: `bravo-${tag}@example.com`,
			role: "admin",
		});
		await openUsers(page);
		const search = page.getByPlaceholder("Search users...");

		await search.fill(tag);
		await expect(rowOf(page, alpha.email)).toBeVisible();
		await expect(rowOf(page, bravo.email)).toBeVisible();

		await search.fill(`Bravo ${tag.toUpperCase()}`);
		await expect(rowOf(page, bravo.email)).toBeVisible();
		await expect(rowOf(page, alpha.email)).toHaveCount(0);

		await search.fill(`alpha-${tag}@`);
		await expect(rowOf(page, alpha.email)).toBeVisible();
		await expect(rowOf(page, bravo.email)).toHaveCount(0);

		await search.fill("admin");
		await expect(rowOf(page, bravo.email)).toBeVisible();
		await expect(rowOf(page, ADMIN_EMAIL)).toBeVisible();
		await expect(rowOf(page, alpha.email)).toHaveCount(0);

		await search.fill("no-such-user");
		await expect(page.getByText("No results.")).toBeVisible();

		await search.fill("");
		await expect(rowOf(page, alpha.email)).toBeVisible();
		await expect(rowOf(page, bravo.email)).toBeVisible();
	});

	test("sorts by a column", async ({ page }) => {
		const tag = uniqueId("sort");
		await createUser(page, { name: `Bravo ${tag}`, email: `3-${tag}@example.com` });
		await createUser(page, { name: `Charlie ${tag}`, email: `1-${tag}@example.com` });
		await createUser(page, { name: `Alpha ${tag}`, email: `2-${tag}@example.com` });
		await openUsers(page);
		await page.getByPlaceholder("Search users...").fill(tag);
		const rows = page.getByRole("row").filter({ hasText: tag });
		await expect(rows).toHaveCount(3);

		const sortBy = async (column: string, direction: "Asc" | "Desc") => {
			await page.getByRole("columnheader", { name: column }).getByRole("button").click();
			await page
				.getByRole("menu", { name: column })
				.getByRole("menuitem", { name: direction, exact: true })
				.click();
		};

		await sortBy("Name", "Asc");
		await expect(rows).toContainText(["Alpha", "Bravo", "Charlie"]);

		await sortBy("Name", "Desc");
		await expect(rows).toContainText(["Charlie", "Bravo", "Alpha"]);

		await sortBy("Email", "Asc");
		await expect(rows).toContainText(["Charlie", "Alpha", "Bravo"]);

		await sortBy("Email", "Desc");
		await expect(rows).toContainText(["Bravo", "Alpha", "Charlie"]);
	});

	test("hides and shows columns", async ({ page }) => {
		await openUsers(page);
		const emailHeader = page.getByRole("columnheader", { name: "Email" });

		await emailHeader.getByRole("button").click();
		await page.getByRole("menuitem", { name: "Hide", exact: true }).click();
		await expect(emailHeader).toHaveCount(0);
		await expect(rowOf(page, ADMIN_NAME)).not.toContainText(ADMIN_EMAIL);

		await page.getByRole("button", { name: "Column Options" }).click();
		await page.getByRole("menuitemcheckbox", { name: /^email$/i }).click();
		await page.keyboard.press("Escape");
		await expect(emailHeader).toBeVisible();
		await expect(rowOf(page, ADMIN_EMAIL)).toBeVisible();
	});
});

test.describe("Access", () => {
	test("regular users can't open the page or use the API", async ({ page, browser }) => {
		const user = await createActiveUser(page, browser);

		expect((await api(user.page, "GET", "/admin/users")).status).toBe(403);
		expect(
			(
				await api(user.page, "POST", "/admin/users", {
					name: "Sneaky Admin",
					email: `sneaky-${user.id}@example.com`,
					role: "admin",
				})
			).status,
		).toBe(403);
		expect(
			(
				await api(user.page, "PUT", `/admin/users/${user.id}`, {
					name: user.name,
					email: user.email,
					role: "admin",
				})
			).status,
		).toBe(403);
		expect((await api(user.page, "DELETE", `/admin/users/${user.id}`)).status).toBe(403);

		await user.page.goto("/admin/users");
		await user.page.waitForURL("/");
		expect((await findUser(page, user.email))?.role).toBe("user");
	});
});
