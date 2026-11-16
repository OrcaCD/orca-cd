import { replaceAuthenticator } from "./helpers/passkeys.ts";
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
	cleanup,
	createActiveUser,
	expectToast,
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
			isAdmin: false,
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
		await signInUi(user.page);
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

	test("the profile and authentication of SSO users are managed by the identity provider", async ({
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
		await expect(
			sso.page.getByText("Authentication for this account is managed by your SSO provider."),
		).toBeVisible();
		await expect(sso.page.getByRole("button", { name: "Add passkey" })).toHaveCount(0);

		const update = await api(sso.page, "PUT", "/auth/profile", {
			name: "Hijacked Name",
			email: sso.profile.email,
		});
		expect(update.status).toBe(403);
		expect(update.body).toEqual({ error: "this operation is not available for sso users" });
		const enrollment = await api(sso.page, "POST", "/auth/passkeys/begin", { name: "Disallowed" });
		expect(enrollment.status).toBe(403);

		expect((await getProfile(sso.page)).name).toBe(sso.mockUser.name);
	});
});

test.describe("Passkeys", () => {
	test("adds a backup key and deletes it while preserving the last key", async ({
		page,
		browser,
	}) => {
		const user = await createActiveUser(page, browser);
		await gotoApp(user.page, "/settings/security");
		await expect(
			user.page.getByRole("button", { name: "Delete passkey", exact: true }),
		).toHaveCount(0);
		await replaceAuthenticator(user.page);
		await user.page.getByLabel("Passkey name", { exact: true }).fill("Backup key");
		await user.page.getByRole("button", { name: "Add passkey", exact: true }).click();
		await expectToast(user.page, "Passkey added");
		await expect(user.page.getByText("Backup key", { exact: true })).toBeVisible();
		const keys = await api(user.page, "GET", "/auth/passkeys");
		expect(keys.body).toHaveLength(2);
		expect(JSON.stringify(keys.body)).not.toContain("publicKey");
		const row = user.page.getByRole("listitem").filter({ hasText: "Backup key" });
		await row.getByRole("button", { name: "Delete passkey", exact: true }).click();
		await user.page
			.getByRole("dialog")
			.getByRole("button", { name: "Delete passkey", exact: true })
			.click();
		await expectToast(user.page, "Passkey deleted");
		await expect(
			user.page.getByRole("button", { name: "Delete passkey", exact: true }),
		).toHaveCount(0);
		const remaining = await api(user.page, "GET", "/auth/passkeys");
		expect(remaining.body).toHaveLength(1);
		expect((await api(user.page, "DELETE", `/auth/passkeys/${remaining.body[0].id}`)).status).toBe(
			400,
		);
	});

	test("canceled prompts can be retried", async ({ page, browser }) => {
		const user = await createActiveUser(page, browser);
		await gotoApp(user.page, "/settings/security");
		await user.page.evaluate(() => {
			navigator.credentials.create = async () => {
				throw new DOMException("Canceled", "NotAllowedError");
			};
		});
		await user.page.getByLabel("Passkey name", { exact: true }).fill("Canceled key");
		await user.page.getByRole("button", { name: "Add passkey", exact: true }).click();
		await expectToast(
			user.page,
			"The passkey request was canceled or timed out. Please try again.",
		);
		expect((await api(user.page, "GET", "/auth/passkeys")).body).toHaveLength(1);
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
			{ path: "/settings/security", key: "passkeyName" },
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
