import { test, expect, type BrowserContext, type Page } from "@playwright/test";
import { decodeJwt } from "jose";
import { gotoApp } from "./helpers/navigation.ts";
import {
	AUTH_COOKIE,
	forgeRawPayload,
	forgeToken,
	hmacTokenWithPublicKey,
	signUserToken,
	unsignedToken,
	userClaims,
	withClaims,
} from "./helpers/jwt.ts";

function omit(claims: Record<string, unknown>, key: string) {
	const { [key]: _, ...rest } = claims;
	return rest;
}

async function getAuthToken(context: BrowserContext) {
	const cookie = (await context.cookies()).find((c) => c.name === AUTH_COOKIE);
	expect(cookie, "the authenticated context has a session cookie").toBeDefined();
	return cookie!.value;
}

async function setAuthToken(context: BrowserContext, value: string) {
	await context.clearCookies();
	await context.addCookies([
		{
			name: AUTH_COOKIE,
			value,
			domain: "127.0.0.1",
			path: "/",
			httpOnly: true,
			secure: true,
			sameSite: "Strict",
		},
	]);
}

async function getUserId(context: BrowserContext) {
	const response = await context.request.get("/api/v1/auth/profile");
	expect(response.status()).toBe(200);
	return (await response.json()).id as string;
}

async function expectSessionRejected(page: Page, context: BrowserContext) {
	const response = await context.request.get("/api/v1/auth/profile");
	expect(response.status()).toBe(401);
	expect((await context.request.get("/api/v1/events")).status()).toBe(401);

	await page.goto("/");
	await page.waitForURL("/login");
	await expect(page).toHaveTitle(/Login/);
}

test.describe("logout", () => {
	test("clears the cookie and the next API call returns 401", async ({ page, context }) => {
		await gotoApp(page, "/");
		await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
		await getAuthToken(context);

		await page
			.getByRole("button")
			.filter({ has: page.locator("[data-slot='avatar']") })
			.click();
		const logoutResponse = page.waitForResponse(
			(r) => r.url().endsWith("/api/v1/auth/logout") && r.request().method() === "POST",
		);
		await page.getByRole("menuitem", { name: "Sign Out" }).click();

		const response = await logoutResponse;
		expect(response.status()).toBe(200);
		const setCookie = (await response.headerValue("set-cookie")) ?? "";
		expect(setCookie).toContain(`${AUTH_COOKIE}=;`);
		expect(setCookie).toContain("Max-Age=0");

		await page.waitForURL("/login");
		expect((await context.cookies()).find((c) => c.name === AUTH_COOKIE)).toBeUndefined();

		for (const path of ["/auth/profile", "/applications", "/repositories", "/agents", "/events"]) {
			expect((await context.request.get(`/api/v1${path}`)).status(), `GET ${path}`).toBe(401);
		}

		await page.goto("/applications");
		await page.waitForURL("/login");
	});
});

test.describe("invalid session cookie", () => {
	test("a valid forged token is accepted, so the helper signs like the hub", async ({
		page,
		context,
	}) => {
		const userId = await getUserId(context);
		await setAuthToken(context, await signUserToken({ subject: userId }));

		expect((await context.request.get("/api/v1/auth/profile")).status()).toBe(200);
		await gotoApp(page, "/");
		await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
	});

	test("an expired token redirects to /login", async ({ page, context }) => {
		const userId = await getUserId(context);
		await setAuthToken(context, await signUserToken({ subject: userId, expiresIn: -60 }));

		const response = await context.request.get("/api/v1/auth/profile");
		expect(response.status()).toBe(401);
		expect((await response.json()).error).toBe("invalid or expired token");

		await page.goto("/");
		await page.waitForURL("/login");
		await expect(page).toHaveTitle(/Login/);
	});

	const tamperedTokens: Record<string, (token: string) => string | Promise<string>> = {
		"a token with a modified payload": (token) => withClaims(token, { role: "admin", exp: 4e9 }),
		"a token with a modified signature": (token) => `${token.slice(0, -4)}AAAA`,
		"a token with a stripped signature": (token) => token.split(".").slice(0, 2).join(".") + ".",
		"a token with only two segments": (token) => token.split(".").slice(0, 2).join("."),
		"a token with an extra segment": (token) => `${token}.${token.split(".")[2]}`,
		"a token with the signature of another token": (token) =>
			`${token.split(".").slice(0, 2).join(".")}.${forgeToken({ sub: "other" }).split(".")[2]}`,
		"an unsigned token with alg none": (token) => unsignedToken(token),
		"a token signed by another key": (token) =>
			signUserToken({
				subject: decodeJwt(token).sub!,
				secret: "another_secret_key_minimum_32_chars",
			}),
		"a random string": () => "not-a-jwt",
		"a string of dots": () => "..",
		"a header-only token": (token) => token.split(".")[0],
	};

	for (const [name, tamper] of Object.entries(tamperedTokens)) {
		test(`${name} redirects to /login`, async ({ page, context }) => {
			const token = await getAuthToken(context);
			// The tampered claims are only meaningful if the original is a valid session
			expect((await context.request.get("/api/v1/auth/profile")).status()).toBe(200);

			await setAuthToken(context, await tamper(token));
			await expectSessionRejected(page, context);
		});
	}

	// Every token is signed with the hub's key, only one thing is wrong with it
	const forgedTokens: Record<string, (userId: string) => string | Promise<string>> = {
		"a token without an alg header": (sub) =>
			forgeToken(userClaims({ subject: sub }), { typ: "JWT" }),
		"a token with an empty alg header": (sub) =>
			forgeToken(userClaims({ subject: sub }), { alg: "", typ: "JWT" }),
		"a token with alg None in different casing": (sub) =>
			forgeToken(userClaims({ subject: sub }), { alg: "None", typ: "JWT" }),
		"a token with alg RS256": (sub) =>
			forgeToken(userClaims({ subject: sub }), { alg: "RS256", typ: "JWT" }),
		"a token with alg HS256 signed with the public key": (sub) =>
			hmacTokenWithPublicKey(userClaims({ subject: sub })),
		"a token with a different audience": (sub) =>
			forgeToken({ ...userClaims({ subject: sub }), aud: ["agent"] }),
		"a token without an audience": (sub) => forgeToken(omit(userClaims({ subject: sub }), "aud")),
		"a token with a different issuer": (sub) =>
			forgeToken({ ...userClaims({ subject: sub }), iss: "https://evil.example.com" }),
		"a token without an issuer": (sub) => forgeToken(omit(userClaims({ subject: sub }), "iss")),
		"a token without an expiry": (sub) => forgeToken(omit(userClaims({ subject: sub }), "exp")),
		"a token that is not valid yet": (sub) =>
			forgeToken({ ...userClaims({ subject: sub }), nbf: Math.floor(Date.now() / 1000) + 3600 }),
		"a token without a not-before claim": (sub) =>
			forgeToken(omit(userClaims({ subject: sub }), "nbf")),
		"a token issued in the future": (sub) =>
			forgeToken({ ...userClaims({ subject: sub }), iat: Math.floor(Date.now() / 1000) + 3600 }),
		"a token without a subject": (sub) => forgeToken(omit(userClaims({ subject: sub }), "sub")),
		"a token with an empty subject": () => forgeToken(userClaims({ subject: "" })),
		"a token with a payload that is not JSON": () => forgeRawPayload("not json"),
		"a token with a payload that is a JSON array": () => forgeRawPayload("[]"),
	};

	for (const [name, forge] of Object.entries(forgedTokens)) {
		test(`${name} redirects to /login`, async ({ page, context }) => {
			const userId = await getUserId(context);

			await setAuthToken(context, await forge(userId));
			await expectSessionRejected(page, context);
		});
	}

	test("a valid token in the Authorization header is ignored", async ({ context }) => {
		const token = await signUserToken({ subject: await getUserId(context) });
		await context.clearCookies();

		for (const path of ["/auth/profile", "/events"]) {
			const response = await context.request.get(`/api/v1${path}`, {
				headers: { Authorization: `Bearer ${token}` },
			});
			expect(response.status(), `GET ${path}`).toBe(401);
		}
	});
});

test.describe("login redirect", () => {
	test.use({ storageState: { cookies: [], origins: [] } });

	test("an unauthenticated visit is sent to /login", async ({ page }) => {
		await page.goto("/applications");
		await page.waitForURL("/login");
	});
});
