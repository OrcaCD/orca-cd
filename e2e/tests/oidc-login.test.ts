import { test, expect, type BrowserContext, type Page } from "@playwright/test";
import { APP_URL } from "./const.ts";
import { api } from "./helpers/api.ts";
import {
	authorizationRequests,
	denyAtMock,
	setMockUser,
	signInAtMock,
	tamperCallback,
	type MockUser,
} from "./helpers/oidc-mock.ts";
import {
	authorizePath,
	callbackPath,
	clickProvider,
	createMockUser,
	createProvider,
	deleteProviders,
	findUsers,
	getProfile,
	listUsers,
	providerButton,
	updateProvider,
	useNewClientIp,
	type ApiOidcProvider,
} from "./helpers/oidc.ts";

// Run all tests in this file without authentication
test.use({ storageState: { cookies: [], origins: [] } });

const LOGIN_ERRORS = {
	access_denied: "Sign-in was canceled or denied by your identity provider.",
	invalid_callback: "The authentication callback was invalid. Please try again.",
	missing_state: "The sign-in session expired. Please start sign-in again.",
	provider_not_found: "The selected identity provider is unavailable.",
	authentication_failed: "Authentication failed. Please try again.",
	email_not_verified: "Your identity provider did not confirm a verified email address.",
	signup_disabled: "Automatic account creation is disabled for this provider.",
	internal_error: "A server error occurred during sign-in. Please try again.",
	account_creation_failed: "Your account could not be created. Please contact an administrator.",
	account_linking_failed: "Your account could not be linked. Please contact an administrator.",
	account_update_failed: "Your account could not be updated. Please contact an administrator.",
	provider_email_conflict:
		"Another account is already linked to this identity provider with the same email. Please contact an administrator.",
	token_generation_failed: "Sign-in failed while creating your session. Please try again.",
} as const;

// Providers are managed through the API as admin, the tests themselves sign in without a session
let adminContext: BrowserContext;
let admin: Page;
const createdProviderIds: string[] = [];

test.beforeAll(async ({ browser }) => {
	adminContext = await browser.newContext({
		baseURL: APP_URL,
		storageState: "playwright/.auth/user.json",
	});
	admin = await adminContext.newPage();
	await admin.goto(`${APP_URL}/api/v1/health`);
});

test.afterAll(async () => {
	try {
		await deleteProviders(admin, createdProviderIds);
	} catch {
		// Leftover providers don't affect other tests, every test uses its own
	} finally {
		await adminContext.close();
	}
});

test.beforeEach(async ({ context }) => {
	await useNewClientIp(context);
});

async function makeProvider(options: Parameters<typeof createProvider>[1] = {}) {
	const provider = await createProvider(admin, options);
	createdProviderIds.push(provider.id);
	return provider;
}

async function signIn(page: Page, provider: ApiOidcProvider, sub: string) {
	await clickProvider(page, provider);
	await signInAtMock(page, sub);
}

async function signOut(page: Page) {
	const { status } = await api(page, "POST", "/auth/logout");
	expect(status).toBe(200);
}

async function expectSignedIn(page: Page) {
	await page.waitForURL("/");
	return getProfile(page);
}

async function expectLoginError(page: Page, error: keyof typeof LOGIN_ERRORS) {
	await expect(page).toHaveURL(`/login?error=${error}`);
	await expect(page.getByRole("alert").filter({ hasText: LOGIN_ERRORS[error] })).toBeVisible();
	expect((await api(page, "GET", "/auth/profile")).status).toBe(401);
}

test.describe("Login page", () => {
	test("lists only enabled providers next to the local login", async ({ page }) => {
		const enabled = await makeProvider();
		const disabled = await makeProvider({ enabled: false });

		await page.goto("/login");

		await expect(providerButton(page, enabled)).toHaveAttribute("href", authorizePath(enabled.id));
		await expect(providerButton(page, disabled)).toHaveCount(0);
		await expect(page.getByRole("textbox", { name: "email" })).toBeVisible();
	});

	for (const [error, message] of Object.entries(LOGIN_ERRORS)) {
		test(`shows a message for the error ${error}`, async ({ page }) => {
			await page.goto(`/login?error=${error}`);
			await expect(page.getByRole("alert").filter({ hasText: message })).toBeVisible();
		});
	}

	test("shows a generic message for unknown errors", async ({ page }) => {
		await page.goto("/login?error=something_unexpected");
		await expect(
			page.getByRole("alert").filter({ hasText: LOGIN_ERRORS.authentication_failed }),
		).toBeVisible();
	});
});

test.describe("Signing in", () => {
	test("creates an account for a new user", async ({ page }) => {
		const provider = await makeProvider();
		const user = await createMockUser({ email: `New.User-${Date.now()}@Example.COM` });

		await signIn(page, provider, user.sub);

		const profile = await expectSignedIn(page);
		expect(profile).toMatchObject({
			name: user.name,
			email: user.email!.toLowerCase(),
			role: "user",
			isLocal: false,
		});
		expect(await findUsers(admin, user.email!.toLowerCase())).toEqual([
			expect.objectContaining({ id: profile.id, role: "user", providers: [provider.name] }),
		]);
	});

	test("asks the identity provider with PKCE, state and nonce", async ({ page }) => {
		const provider = await makeProvider({ scopes: "groups" });
		const user = await createMockUser({ groups: ["admins"] });

		await signIn(page, provider, user.sub);
		await expectSignedIn(page);

		const requests = await authorizationRequests(provider.clientId);
		expect(requests).toHaveLength(1);
		const [request] = requests;
		expect(request).toMatchObject({
			client_id: provider.clientId,
			redirect_uri: provider.callbackUrl,
			response_type: "code",
			code_challenge_method: "S256",
		});
		expect(request.scope.split(" ")).toEqual(["openid", "email", "profile", "groups"]);
		expect(request.state.length).toBeGreaterThanOrEqual(32);
		expect(request.nonce.length).toBeGreaterThanOrEqual(32);
		expect(request.code_challenge).toHaveLength(43);
	});

	test("uses the email as name if the identity provider sends none", async ({ page }) => {
		const provider = await makeProvider();
		const user = await createMockUser({ name: undefined });

		await signIn(page, provider, user.sub);

		expect(await expectSignedIn(page)).toMatchObject({ name: user.email, email: user.email });
	});

	test("only accepts http(s) profile pictures", async ({ page }) => {
		const provider = await makeProvider();
		const withPicture = await createMockUser({ picture: "https://example.com/avatar.png" });
		const withInvalidPicture = await createMockUser({ picture: "javascript:alert(1)" });

		await signIn(page, provider, withPicture.sub);
		expect((await expectSignedIn(page)).picture).toBeTruthy();
		await signOut(page);

		await signIn(page, provider, withInvalidPicture.sub);
		expect((await expectSignedIn(page)).picture).toBeUndefined();
	});

	test("signs a returning user into the same account and syncs name and email", async ({
		page,
	}) => {
		const provider = await makeProvider();
		const user = await createMockUser();

		await signIn(page, provider, user.sub);
		const first = await expectSignedIn(page);
		await signOut(page);

		const changed: MockUser = {
			email: `changed-${user.sub}@example.com`,
			email_verified: true,
			name: "Changed Name",
		};
		await setMockUser(user.sub, changed);

		await signIn(page, provider, user.sub);
		const second = await expectSignedIn(page);

		expect(second).toMatchObject({ id: first.id, name: "Changed Name", email: changed.email });
		expect(await findUsers(admin, user.email!)).toEqual([]);
		expect(await findUsers(admin, changed.email!)).toHaveLength(1);
	});

	test("links an existing account with the same email", async ({ page }) => {
		const first = await makeProvider();
		const second = await makeProvider({ autoSignup: false });
		const user = await createMockUser();

		await signIn(page, first, user.sub);
		const created = await expectSignedIn(page);
		await signOut(page);

		// An existing account is linked even though new accounts are not created
		await signIn(page, second, user.sub);
		const linked = await expectSignedIn(page);

		expect(linked.id).toBe(created.id);
		const accounts = await findUsers(admin, user.email!);
		expect(accounts).toHaveLength(1);
		expect(accounts[0].providers).toEqual(expect.arrayContaining([first.name, second.name]));
	});

	test("does not give SSO users access to the admin area", async ({ page }) => {
		const provider = await makeProvider();
		const user = await createMockUser();

		await signIn(page, provider, user.sub);
		await expectSignedIn(page);

		expect((await api(page, "GET", "/admin/oidc-providers")).status).toBe(403);
		expect((await api(page, "GET", "/admin/users")).status).toBe(403);
		expect(
			(
				await api(page, "POST", "/admin/oidc-providers", {
					name: "Evil",
					issuerUrl: provider.issuerUrl,
					clientId: "evil",
					clientSecret: "evil",
				})
			).status,
		).toBe(403);
		expect((await api(page, "DELETE", `/admin/oidc-providers/${provider.id}`)).status).toBe(403);
	});
});

test.describe("Account creation", () => {
	test("is refused if auto signup is disabled", async ({ page }) => {
		const provider = await makeProvider({ autoSignup: false });
		const user = await createMockUser();

		await signIn(page, provider, user.sub);

		await expectLoginError(page, "signup_disabled");
		expect(await findUsers(admin, user.email!)).toEqual([]);
	});

	test("is not needed for users that signed in before", async ({ page }) => {
		const provider = await makeProvider();
		const user = await createMockUser();

		await signIn(page, provider, user.sub);
		const created = await expectSignedIn(page);
		await signOut(page);

		await updateProvider(admin, provider, { autoSignup: false });

		await signIn(page, provider, user.sub);
		expect((await expectSignedIn(page)).id).toBe(created.id);
	});

	test("works again once auto signup is enabled", async ({ page }) => {
		const provider = await makeProvider();
		const user = await createMockUser();
		await updateProvider(admin, provider, { autoSignup: false });

		await signIn(page, provider, user.sub);
		await expectLoginError(page, "signup_disabled");

		await updateProvider(admin, provider, { autoSignup: true });

		await signIn(page, provider, user.sub);
		expect((await expectSignedIn(page)).email).toBe(user.email);
	});

	test("is refused if a second identity of the provider would be linked to an account", async ({
		page,
	}) => {
		const provider = await makeProvider();
		const email = `shared-${Date.now()}@example.com`;
		const first = await createMockUser({ email });
		const second = await createMockUser({ email });

		await signIn(page, provider, first.sub);
		await expectSignedIn(page);
		await signOut(page);

		await signIn(page, provider, second.sub);

		await expectLoginError(page, "account_linking_failed");
		expect(await findUsers(admin, email)).toHaveLength(1);
	});

	test("is refused if the email changed to the one of another account", async ({ page }) => {
		const provider = await makeProvider();
		const first = await createMockUser();
		const second = await createMockUser();

		await signIn(page, provider, first.sub);
		await expectSignedIn(page);
		await signOut(page);
		await signIn(page, provider, second.sub);
		const secondAccount = await expectSignedIn(page);
		await signOut(page);

		await setMockUser(second.sub, { ...second, email: first.email });
		await signIn(page, provider, second.sub);

		await expectLoginError(page, "provider_email_conflict");
		// Neither account was changed
		expect(await findUsers(admin, first.email!)).toHaveLength(1);
		expect(await findUsers(admin, second.email!)).toEqual([
			expect.objectContaining({ id: secondAccount.id }),
		]);
	});
});

test.describe("Verified email", () => {
	const unverified: Record<string, Partial<MockUser>> = {
		"is false": { email_verified: false },
		"is missing": { email_verified: undefined },
	};

	for (const [title, claims] of Object.entries(unverified)) {
		test(`is required by default, the claim ${title}`, async ({ page }) => {
			const provider = await makeProvider();
			const user = await createMockUser(claims);

			await signIn(page, provider, user.sub);

			await expectLoginError(page, "email_not_verified");
			expect(await findUsers(admin, user.email!)).toEqual([]);
		});

		test(`can be optional, the claim ${title}`, async ({ page }) => {
			const provider = await makeProvider({ requireVerifiedEmail: false });
			const user = await createMockUser(claims);

			await signIn(page, provider, user.sub);

			expect(await expectSignedIn(page)).toMatchObject({ email: user.email });
		});
	}

	test("is enforced after the setting changed", async ({ page }) => {
		const provider = await makeProvider({ requireVerifiedEmail: false });
		const user = await createMockUser({ email_verified: false });

		await signIn(page, provider, user.sub);
		await expectSignedIn(page);
		await signOut(page);

		await updateProvider(admin, provider, { requireVerifiedEmail: true });

		await signIn(page, provider, user.sub);
		await expectLoginError(page, "email_not_verified");
	});
});

test.describe("Failures at the identity provider", () => {
	test("show a message if the user denies the access", async ({ page }) => {
		const provider = await makeProvider();
		await createMockUser();

		await clickProvider(page, provider);
		await denyAtMock(page);

		await expectLoginError(page, "access_denied");
	});

	test("fail if the id_token has no email", async ({ page }) => {
		const provider = await makeProvider();
		const user = await createMockUser({ email: undefined, email_verified: undefined });

		await signIn(page, provider, user.sub);

		await expectLoginError(page, "authentication_failed");
		expect((await listUsers(admin)).filter((item) => item.name === user.name)).toEqual([]);
	});

	test("fail if the client secret is wrong", async ({ page }) => {
		const provider = await makeProvider({ wrongSecret: true });
		const user = await createMockUser();

		await signIn(page, provider, user.sub);

		await expectLoginError(page, "authentication_failed");
		expect(await findUsers(admin, user.email!)).toEqual([]);
	});
});

test.describe("Client secret", () => {
	test("is kept if an update does not contain a new one", async ({ page }) => {
		const provider = await makeProvider();
		const user = await createMockUser();

		await updateProvider(admin, provider, { name: `${provider.name} renamed` });

		await signIn(page, { ...provider, name: `${provider.name} renamed` }, user.sub);
		await expectSignedIn(page);
	});

	test("is replaced if an update contains a new one", async ({ page }) => {
		const provider = await makeProvider();
		const user = await createMockUser();

		await updateProvider(admin, provider, { clientSecret: "replaced-secret" });
		await signIn(page, provider, user.sub);
		await expectLoginError(page, "authentication_failed");

		await updateProvider(admin, provider, { clientSecret: provider.clientSecret });
		await signIn(page, provider, user.sub);
		await expectSignedIn(page);
	});
});

test.describe("Disabled and unknown providers", () => {
	test("can not start a login", async ({ page }) => {
		const disabled = await makeProvider({ enabled: false });

		for (const id of [disabled.id, "does-not-exist"]) {
			const response = await page.request.get(authorizePath(id), { maxRedirects: 0 });
			expect(response.status()).toBe(404);
			expect(await response.json()).toEqual({ error: "provider not found" });
		}
	});

	test("can not finish a login", async ({ page }) => {
		const provider = await makeProvider();
		const user = await createMockUser();

		await clickProvider(page, provider);
		await updateProvider(admin, provider, { enabled: false });
		await signInAtMock(page, user.sub);

		await expectLoginError(page, "provider_not_found");
		expect(await findUsers(admin, user.email!)).toEqual([]);
	});

	test("can be enabled again", async ({ page }) => {
		const provider = await makeProvider({ enabled: false });
		const user = await createMockUser();

		await updateProvider(admin, provider, { enabled: true });

		await signIn(page, provider, user.sub);
		await expectSignedIn(page);
	});

	test("can not be used after they were deleted", async ({ page }) => {
		const provider = await makeProvider();
		const user = await createMockUser();

		await signIn(page, provider, user.sub);
		const profile = await expectSignedIn(page);
		await signOut(page);

		await deleteProviders(admin, [provider.id]);

		const response = await page.request.get(authorizePath(provider.id), { maxRedirects: 0 });
		expect(response.status()).toBe(404);
		// The account stays, only the way to sign in with it is gone
		expect((await listUsers(admin)).some((item) => item.id === profile.id)).toBe(true);
	});
});

test.describe("Callback validation", () => {
	let provider: ApiOidcProvider;

	test.beforeAll(async () => {
		provider = await makeProvider();
	});

	test("rejects a callback without code and state", async ({ page }) => {
		await page.goto(callbackPath(provider.id));
		await expectLoginError(page, "invalid_callback");

		await page.goto(`${callbackPath(provider.id)}?code=abc`);
		await expectLoginError(page, "invalid_callback");

		await page.goto(`${callbackPath(provider.id)}?state=abc`);
		await expectLoginError(page, "invalid_callback");
	});

	test("rejects a callback without a started login", async ({ page }) => {
		await page.goto(`${callbackPath(provider.id)}?code=abc&state=abc`);
		await expectLoginError(page, "missing_state");
	});

	test("forwards errors of the identity provider", async ({ page }) => {
		await page.goto(`${callbackPath(provider.id)}?error=access_denied`);
		await expectLoginError(page, "access_denied");

		await page.goto(`${callbackPath(provider.id)}?error=server_error`);
		await expect(page).toHaveURL("/login?error=server_error");
		await expect(
			page.getByRole("alert").filter({ hasText: LOGIN_ERRORS.authentication_failed }),
		).toBeVisible();
	});

	test("rejects a changed state", async ({ page }) => {
		const tampered = await makeProvider();
		const user = await createMockUser();
		await tamperCallback(tampered.clientId, { state: "tampered" });

		await signIn(page, tampered, user.sub);

		await expectLoginError(page, "authentication_failed");
		expect(await findUsers(admin, user.email!)).toEqual([]);
	});

	test("rejects a state that was issued for another provider", async ({ page }) => {
		const tampered = await makeProvider();
		const other = await makeProvider();
		const user = await createMockUser();
		await tamperCallback(tampered.clientId, { callbackPath: callbackPath(other.id) });

		await signIn(page, tampered, user.sub);

		await expectLoginError(page, "authentication_failed");
		expect(await findUsers(admin, user.email!)).toEqual([]);
	});

	test("rejects a replayed callback", async ({ page }) => {
		const user = await createMockUser();
		const callback = page.waitForRequest(
			(request) => new URL(request.url()).pathname === callbackPath(provider.id),
		);

		await signIn(page, provider, user.sub);
		await expectSignedIn(page);

		const replay = await page.request.get((await callback).url(), { maxRedirects: 0 });
		expect(replay.status()).toBe(302);
		expect(replay.headers()["location"]).toBe("/login?error=missing_state");
	});

	test("clears the state when the login was finished", async ({ page }) => {
		const user = await createMockUser();

		await signIn(page, provider, user.sub);
		await expectSignedIn(page);

		const cookies = await page.context().cookies(APP_URL);
		expect(cookies.map((cookie) => cookie.name)).not.toContain("orcacd_oidc_state");
	});
});
