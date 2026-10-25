import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test, expect, type Browser, type Page } from "@playwright/test";
import { ADMIN_EMAIL } from "./const.ts";
import { api } from "./helpers/api.ts";
import { gotoApp } from "./helpers/navigation.ts";
import {
	OIDC_MOCK_AVATAR_URL,
	OIDC_MOCK_MISSING_IMAGE_URL,
	OIDC_MOCK_NOT_AN_IMAGE_URL,
	signInAtMock,
} from "./helpers/oidc-mock.ts";
import {
	clickProvider,
	createMockUser,
	createProvider,
	deleteProviders,
	findUsers,
	getProfile,
	listUsers,
	uniqueId,
} from "./helpers/oidc.ts";
import {
	ACTIVE_PASSWORD,
	NEW_PASSWORD,
	apiSignIn,
	cleanup,
	createActiveUser,
	expectToast,
	fillPasswords,
	openSession,
	signInUi,
	signOutUi,
	trackUser,
} from "./helpers/users.ts";

const providerIds: string[] = [];

test.afterEach(async ({ page }) => {
	await cleanup(page);
	await deleteProviders(page, providerIds.splice(0));
});

async function signInWithSso(admin: Page, browser: Browser, picture?: string) {
	const provider = await createProvider(admin);
	providerIds.push(provider.id);
	const mockUser = await createMockUser({ picture });
	const session = await openSession(browser);

	await clickProvider(session.page, provider);
	await signInAtMock(session.page, mockUser.sub);
	await session.page.waitForURL("/");

	const profile = await getProfile(session.page);
	trackUser(profile.id);
	return { ...session, mockUser, profile };
}

const avatarImage = (page: Page) => page.locator("header [data-slot='avatar'] img");

test.describe("Profile", () => {
	test("changes name and email", async ({ page, browser }) => {
		const user = await createActiveUser(page, browser);
		const id = uniqueId("profile");
		const name = `Renamed ${id}`;
		const email = `Renamed-${id}@Example.com`;
		await gotoApp(user.page, "/settings/profile");
		await expect(user.page).toHaveTitle(/Settings - Profile/);
		const nameInput = user.page.getByLabel("Name", { exact: true });
		const emailInput = user.page.getByLabel("Email", { exact: true });
		await expect(nameInput).toHaveValue(user.name);
		await expect(emailInput).toHaveValue(user.email);

		await nameInput.fill(name);
		await emailInput.fill(email);
		await user.page.getByRole("button", { name: "Save Changes" }).click();

		await expectToast(user.page, "Profile updated");
		expect(await getProfile(user.page)).toMatchObject({
			id: user.id,
			name,
			email: email.toLowerCase(),
			role: "user",
			isLocal: true,
		});
		expect((await listUsers(page)).find((item) => item.id === user.id)).toMatchObject({
			name,
			email: email.toLowerCase(),
		});

		await user.page.reload();
		await expect(nameInput).toHaveValue(name);
		await expect(emailInput).toHaveValue(email.toLowerCase());

		await signOutUi(user.page);
		await signInUi(user.page, email, ACTIVE_PASSWORD);
		await user.page.waitForURL("/");
		expect((await getProfile(user.page)).name).toBe(name);
	});

	test("validates the form before sending it", async ({ page, browser }) => {
		const user = await createActiveUser(page, browser);
		await gotoApp(user.page, "/settings/profile");
		let updates = 0;
		user.page.on("request", (request) => {
			if (request.method() === "PUT") {
				updates++;
			}
		});

		await user.page.getByLabel("Name", { exact: true }).fill("ab");
		await user.page.getByLabel("Email", { exact: true }).fill("user@localhost");
		await user.page.getByRole("button", { name: "Save Changes" }).click();

		await expect(user.page.getByText("Name must be at least 3 characters")).toBeVisible();
		await expect(user.page.getByText("Must be a valid email address")).toBeVisible();
		expect(updates).toBe(0);
		expect(await getProfile(user.page)).toMatchObject({ name: user.name, email: user.email });
	});

	test("rejects an email that is already used", async ({ page, browser }) => {
		const user = await createActiveUser(page, browser);
		await gotoApp(user.page, "/settings/profile");

		await user.page.getByLabel("Email", { exact: true }).fill(ADMIN_EMAIL);
		await user.page.getByRole("button", { name: "Save Changes" }).click();

		await expectToast(user.page, "a user with this email already exists");
		expect((await getProfile(user.page)).email).toBe(user.email);
		expect(await findUsers(page, ADMIN_EMAIL)).toHaveLength(1);
	});

	test("local users have no profile picture", async ({ page, browser }) => {
		const user = await createActiveUser(page, browser);
		await gotoApp(user.page, "/");

		const response = await user.page.request.get("/api/v1/auth/profile-picture");
		expect(response.status()).toBe(404);
		expect(await response.json()).toEqual({ error: "no profile picture available" });
		expect((await getProfile(user.page)).picture).toBeUndefined();
		await expect(avatarImage(user.page)).toHaveCount(0);
		await expect(user.page.locator("header [data-slot='avatar-fallback']")).toBeVisible();
	});

	test("the profile picture of an SSO user is proxied by the hub", async ({ page, browser }) => {
		const sso = await signInWithSso(page, browser, OIDC_MOCK_AVATAR_URL);

		expect(sso.profile.picture).toBe("/api/v1/auth/profile-picture");
		await expect(avatarImage(sso.page)).toHaveAttribute("src", "/api/v1/auth/profile-picture");

		const response = await sso.page.request.get("/api/v1/auth/profile-picture");
		expect(response.status()).toBe(200);
		expect(response.headers()["content-type"]).toBe("image/svg+xml");
		expect(response.headers()["content-disposition"]).toContain("attachment");
		expect(response.headers()["content-security-policy"]).toBe("default-src 'none'; sandbox");
		expect(response.headers()["cache-control"]).toContain("private");
		expect(await response.text()).toContain("<svg");
	});

	test("the navbar falls back to a generated avatar if the picture can't be loaded", async ({
		page,
		browser,
	}) => {
		const notAnImage = await signInWithSso(page, browser, OIDC_MOCK_NOT_AN_IMAGE_URL);
		const notAnImageResponse = await notAnImage.page.request.get("/api/v1/auth/profile-picture");
		expect(notAnImageResponse.status()).toBe(502);
		expect(await notAnImageResponse.json()).toEqual({
			error: "profile picture provider did not return an image",
		});
		await expect(avatarImage(notAnImage.page)).toHaveCount(0);
		await expect(notAnImage.page.locator("header [data-slot='avatar-fallback']")).toBeVisible();

		const missing = await signInWithSso(page, browser, OIDC_MOCK_MISSING_IMAGE_URL);
		const missingResponse = await missing.page.request.get("/api/v1/auth/profile-picture");
		expect(missingResponse.status()).toBe(502);
		expect(await missingResponse.json()).toEqual({
			error: "profile picture provider returned an error",
		});

		const none = await signInWithSso(page, browser);
		expect((await none.page.request.get("/api/v1/auth/profile-picture")).status()).toBe(404);
	});

	test("the profile and the password of SSO users are managed by the identity provider", async ({
		page,
		browser,
	}) => {
		const sso = await signInWithSso(page, browser, OIDC_MOCK_AVATAR_URL);

		await gotoApp(sso.page, "/settings/profile");
		await expect(sso.page.getByText("Managed Profile")).toBeVisible();
		await expect(sso.page.getByLabel("Name", { exact: true })).toBeDisabled();
		await expect(sso.page.getByLabel("Email", { exact: true })).toBeDisabled();
		await expect(sso.page.getByRole("button", { name: "Save Changes" })).toBeDisabled();

		await gotoApp(sso.page, "/settings/security");
		await expect(sso.page.getByText("Managed Profile")).toBeVisible();
		await expect(sso.page.getByLabel("Current Password", { exact: true })).toBeDisabled();
		await expect(sso.page.getByRole("button", { name: "Update Password" })).toBeDisabled();

		const update = await api(sso.page, "PUT", "/auth/profile", {
			name: "Hijacked Name",
			email: sso.profile.email,
		});
		expect(update.status).toBe(403);
		expect(update.body).toEqual({ error: "this operation is not available for sso users" });

		const password = await api(sso.page, "POST", "/auth/change-password", {
			currentPassword: "Whatever-Password-1!",
			newPassword: NEW_PASSWORD,
		});
		expect(password.status).toBe(400);
		expect(password.body).toEqual({ error: "cannot change password for managed user" });
		expect((await getProfile(sso.page)).name).toBe(sso.mockUser.name);
	});
});

test.describe("Password", () => {
	test("is validated before it is sent", async ({ page, browser }) => {
		const user = await createActiveUser(page, browser);
		let changes = 0;
		user.page.on("request", (request) => {
			if (new URL(request.url()).pathname === "/api/v1/auth/change-password") {
				changes++;
			}
		});

		const cases: { title: string; values: Parameters<typeof fillPasswords>[1]; message: string }[] =
			[
				{
					title: "an empty current password",
					values: { current: "", next: NEW_PASSWORD, confirm: NEW_PASSWORD },
					message: "Current password is required",
				},
				{
					title: "a short password",
					values: { current: ACTIVE_PASSWORD, next: "Short1!", confirm: "Short1!" },
					message: "New password must be at least 12 characters",
				},
				{
					title: "a long password",
					values: {
						current: ACTIVE_PASSWORD,
						next: `Aa1!${"x".repeat(125)}`,
						confirm: `Aa1!${"x".repeat(125)}`,
					},
					message: "New password must be at most 128 characters",
				},
				{
					title: "a password without an uppercase letter",
					values: { current: ACTIVE_PASSWORD, next: "alllowercase1!", confirm: "alllowercase1!" },
					message: "New password must contain at least one uppercase letter",
				},
				{
					title: "a password without a lowercase letter",
					values: { current: ACTIVE_PASSWORD, next: "ALLUPPERCASE1!", confirm: "ALLUPPERCASE1!" },
					message: "New password must contain at least one lowercase letter",
				},
				{
					title: "a password without a number",
					values: { current: ACTIVE_PASSWORD, next: "NoNumbersHere!!", confirm: "NoNumbersHere!!" },
					message: "New password must contain at least one number",
				},
				{
					title: "a password without a special character",
					values: { current: ACTIVE_PASSWORD, next: "NoSpecial12345A", confirm: "NoSpecial12345A" },
					message: "New password must contain at least one special character",
				},
				{
					title: "a missing confirmation",
					values: { current: ACTIVE_PASSWORD, next: NEW_PASSWORD, confirm: "" },
					message: "Please confirm your new password",
				},
				{
					title: "a different confirmation",
					values: { current: ACTIVE_PASSWORD, next: NEW_PASSWORD, confirm: `${NEW_PASSWORD}x` },
					message: "Passwords do not match",
				},
			];

		for (const { title, values, message } of cases) {
			await test.step(title, async () => {
				await gotoApp(user.page, "/settings/security");
				await fillPasswords(user.page, values);
				await user.page.getByRole("button", { name: "Update Password" }).click();

				await expect(user.page.getByText(message, { exact: true })).toBeVisible();
			});
		}

		expect(changes).toBe(0);
	});

	test("can be shown and hidden", async ({ page, browser }) => {
		const user = await createActiveUser(page, browser);
		await gotoApp(user.page, "/settings/security");
		const current = user.page.getByLabel("Current Password", { exact: true });
		await current.fill(ACTIVE_PASSWORD);
		await expect(current).toHaveAttribute("type", "password");

		await user.page.getByRole("button", { name: "Show password" }).first().click();
		await expect(current).toHaveAttribute("type", "text");

		await user.page.getByRole("button", { name: "Hide password" }).first().click();
		await expect(current).toHaveAttribute("type", "password");
	});

	test("must be the current password to change it", async ({ page, browser }) => {
		const user = await createActiveUser(page, browser);
		await gotoApp(user.page, "/settings/security");

		await fillPasswords(user.page, {
			current: "Wrong-Password-123!",
			next: NEW_PASSWORD,
			confirm: NEW_PASSWORD,
		});
		await user.page.getByRole("button", { name: "Update Password" }).click();

		await expectToast(user.page, "current password is incorrect");
		expect((await getProfile(user.page)).id).toBe(user.id);
		const { page: other } = await openSession(browser);
		await apiSignIn(other, user.email, ACTIVE_PASSWORD);
	});

	test("must differ from the current password", async ({ page, browser }) => {
		const user = await createActiveUser(page, browser);
		await gotoApp(user.page, "/settings/security");

		await fillPasswords(user.page, {
			current: ACTIVE_PASSWORD,
			next: ACTIVE_PASSWORD,
			confirm: ACTIVE_PASSWORD,
		});
		await user.page.getByRole("button", { name: "Update Password" }).click();

		await expectToast(user.page, "new password must be different from current password");
	});

	test("can be changed and the new one is used for the next sign in", async ({ page, browser }) => {
		const user = await createActiveUser(page, browser);
		const other = await openSession(browser);
		await apiSignIn(other.page, user.email, ACTIVE_PASSWORD);
		await gotoApp(user.page, "/settings/security");

		await fillPasswords(user.page, {
			current: ACTIVE_PASSWORD,
			next: NEW_PASSWORD,
			confirm: NEW_PASSWORD,
		});
		await user.page.getByRole("button", { name: "Update Password" }).click();

		await expectToast(user.page, "Password updated successfully");
		await expect(user.page.getByLabel("Current Password", { exact: true })).toHaveValue("");
		await expect(user.page.getByLabel("New Password", { exact: true })).toHaveValue("");
		await expect(user.page.getByLabel("Confirm New Password", { exact: true })).toHaveValue("");
		expect((await getProfile(user.page)).id).toBe(user.id);
		expect((await api(other.page, "GET", "/auth/profile")).status).toBe(401);

		await signOutUi(user.page);
		await signInUi(user.page, user.email, ACTIVE_PASSWORD);
		await expect(user.page.getByText("invalid email or password")).toBeVisible();

		await signInUi(user.page, user.email, NEW_PASSWORD);
		await user.page.waitForURL("/");
		await expect(user.page.getByRole("heading", { name: "Overview" })).toBeVisible();
	});
});

test.describe("Language", () => {
	test("is switched to German and back and survives a reload", async ({ page }) => {
		await gotoApp(page, "/settings/profile");
		await expect(page.getByText("Personal Information", { exact: true })).toBeVisible();
		await expect(page.locator("html")).toHaveAttribute("lang", "en");

		await page.locator("#language-select").click();
		await page.getByRole("option", { name: "German" }).click();

		await expect(page.getByText("Persönliche Informationen", { exact: true })).toBeVisible();
		await expect(page.getByText("Personal Information", { exact: true })).toHaveCount(0);
		await expect(page.locator("html")).toHaveAttribute("lang", "de");
		await expect(page).toHaveTitle(/Einstellungen - Profil/);

		await page.reload();
		await expect(page.getByText("Persönliche Informationen", { exact: true })).toBeVisible();
		await expect(page.locator("#language-select")).toContainText("Deutsch");

		await page.getByRole("link", { name: "Anwendungen" }).first().click();
		await page.waitForURL("/applications");
		await expect(page.getByRole("link", { name: "Applications", exact: true })).toHaveCount(0);

		await gotoApp(page, "/settings/profile");
		await page.locator("#language-select").click();
		await page.getByRole("option", { name: "Englisch" }).click();

		await expect(page.getByText("Personal Information", { exact: true })).toBeVisible();
		await expect(page.locator("html")).toHaveAttribute("lang", "en");
	});

	test.describe("German translation", () => {
		const de = JSON.parse(
			readFileSync(join(import.meta.dirname, "../../frontend/messages/de.json"), "utf8"),
		) as Record<string, string>;
		const en = JSON.parse(
			readFileSync(join(import.meta.dirname, "../../frontend/messages/en.json"), "utf8"),
		) as Record<string, string>;

		test.beforeEach(async ({ page }) => {
			await page.addInitScript(() => localStorage.setItem("orca-locale", "de"));
		});

		const pages = [
			{ path: "/", key: "pageOverview", role: "heading" as const },
			{ path: "/settings/profile", key: "personalInformation" },
			{ path: "/settings/security", key: "changePassword" },
			{ path: "/settings/security", key: "sessions" },
			{ path: "/admin/users", key: "searchUsers", placeholder: true },
			{ path: "/admin/oidc-providers", key: "adminOidcProviders" },
		];

		for (const { path, key, role, placeholder } of pages) {
			test(`${path} renders ${key} in German`, async ({ page }) => {
				expect(de[key], `${key} is translated`).toBeTruthy();
				expect(de[key]).not.toBe(en[key]);

				await gotoApp(page, path);

				await expect(page.locator("html")).toHaveAttribute("lang", "de");
				const target = placeholder
					? page.getByPlaceholder(de[key])
					: role
						? page.getByRole(role, { name: de[key], exact: true })
						: page.getByText(de[key], { exact: true }).first();
				await expect(target).toBeVisible();
			});
		}

		test("the navigation is German", async ({ page }) => {
			await gotoApp(page, "/");

			for (const key of ["navApplications", "navAgents", "navRepositories", "navNotifications"]) {
				await expect(
					page.getByRole("link", { name: de[key], exact: true }).first(),
					`${key} in the navigation`,
				).toBeVisible();
			}
		});

		test("the login page is German", async ({ browser }) => {
			const { page } = await openSession(browser);
			await page.addInitScript(() => localStorage.setItem("orca-locale", "de"));

			await page.goto("/login");

			await expect(page.locator("html")).toHaveAttribute("lang", "de");
			await expect(
				page.getByRole("heading", { name: de.loginToYourAccount, exact: true }),
			).toBeVisible();
			await expect(page.getByText(en.loginToYourAccount, { exact: true })).toHaveCount(0);
		});
	});
});

test.describe("Theme", () => {
	test("is toggled and survives a reload", async ({ page }) => {
		await gotoApp(page, "/");
		const html = page.locator("html");
		const toLight = page.getByRole("button", { name: "Switch to Light theme" });
		const toDark = page.getByRole("button", { name: "Switch to Dark theme" });

		await expect(html).toHaveClass(/\bdark\b/);
		await expect(toLight).toBeVisible();

		await toLight.click();
		await expect(html).toHaveClass(/\blight\b/);
		await expect(html).not.toHaveClass(/\bdark\b/);
		await expect(toDark).toBeVisible();
		expect(await page.evaluate(() => localStorage.getItem("orca-theme"))).toBe("light");

		await page.reload();
		await expect(html).toHaveClass(/\blight\b/);
		await expect(toDark).toBeVisible();

		await gotoApp(page, "/settings/profile");
		await expect(html).toHaveClass(/\blight\b/);

		await toDark.click();
		await expect(html).toHaveClass(/\bdark\b/);
		expect(await page.evaluate(() => localStorage.getItem("orca-theme"))).toBe("dark");

		await page.reload();
		await expect(html).toHaveClass(/\bdark\b/);
		await expect(toLight).toBeVisible();
	});

	test("follows the system preference if it was set to system", async ({ page }) => {
		await page.addInitScript(() => localStorage.setItem("orca-theme", "system"));
		await page.emulateMedia({ colorScheme: "light" });
		await gotoApp(page, "/");
		const html = page.locator("html");

		await expect(html).toHaveClass(/\blight\b/);

		await page.emulateMedia({ colorScheme: "dark" });
		await page.reload();
		await expect(html).toHaveClass(/\bdark\b/);
	});
});
