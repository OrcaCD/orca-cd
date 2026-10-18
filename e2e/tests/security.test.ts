import { test, expect } from "@playwright/test";
import { APP_URL } from "./const.ts";
import { readSetupCode } from "./helpers/setup-code.ts";

// Run all tests in this file without authentication
test.use({ storageState: { cookies: [], origins: [] } });

test.describe("registration is disabled after initial setup", () => {
	test("registration is disabled", async ({ request }) => {
		const response = await request.get("/api/v1/auth/setup");
		expect(response.status()).toBe(200);
		const body = await response.json();
		expect(body.needsSetup).toBe(false);
	});

	test("registration is blocked", async ({ request }) => {
		const response = await request.post("/api/v1/auth/register", {
			data: { email: "attacker@example.com", password: "Password123456!", name: "Attacker" },
			headers: {
				Origin: APP_URL,
			},
		});
		expect(response.status()).toBe(403);
		const body = await response.json();
		expect(body.error).toBe("invalid setup code");
	});

	test("registration is blocked even with the initial setup code", async ({ request }) => {
		const setupCode = await readSetupCode();
		const response = await request.post("/api/v1/auth/register", {
			data: {
				email: "attacker@example.com",
				password: "Password123456!",
				name: "Attacker",
				setupCode,
			},
			headers: {
				Origin: APP_URL,
			},
		});
		expect(response.status()).toBe(403);
	});
});

test.describe("security headers", () => {
	test("are present on every response", async ({ request }) => {
		const response = await request.get("/api/v1/health");
		const h = response.headers();

		expect(h["x-content-type-options"]).toBe("nosniff");
		expect(h["x-frame-options"]).toBe("DENY");
		expect(h["referrer-policy"]).toBe("same-origin");
		expect(h["cross-origin-opener-policy"]).toBe("same-origin");
		expect(h["cross-origin-resource-policy"]).toBe("same-origin");
		expect(h["x-powered-by"]).toBeUndefined();
		expect(h["x-robots-tag"]).toBe("noindex, nofollow, noarchive");
	});
});

test.describe("Origin CSRF check works as expected", () => {
	test("blocks requests with missing Origin header", async ({ request }) => {
		const response = await request.post("/api/v1/auth/login");
		expect(response.status()).toBe(403);
		const body = await response.text();
		expect(body).toContain("missing origin header");
	});
	test("blocks requests with invalid Origin header", async ({ request }) => {
		const response = await request.post("/api/v1/auth/login", {
			headers: {
				ORIGIN: "http://malicious-website.com",
			},
		});
		expect(response.status()).toBe(403);
		const body = await response.text();
		expect(body).toContain("invalid origin");
	});
});

test.describe("protected API routes require authentication", () => {
	test("GET /api/v1/repositories returns 401", async ({ request }) => {
		expect((await request.get("/api/v1/repositories")).status()).toBe(401);
	});
	test("GET /api/v1/repositories/:id returns 401", async ({ request }) => {
		expect((await request.get("/api/v1/repositories/some-id")).status()).toBe(401);
	});
	test("POST /api/v1/repositories/test-connection returns 401", async ({ request }) => {
		expect(
			(
				await request.post("/api/v1/repositories/test-connection", {
					data: { url: "https://example.com/org/repo", provider: "gitea", authMethod: "none" },
					headers: { Origin: APP_URL },
				})
			).status(),
		).toBe(401);
	});
	test("DELETE /api/v1/repositories/:id returns 401", async ({ request }) => {
		expect(
			(
				await request.delete("/api/v1/repositories/some-id", { headers: { Origin: APP_URL } })
			).status(),
		).toBe(401);
	});
	test("GET /api/v1/events (SSE) returns 401", async ({ request }) => {
		const response = await request.get("/api/v1/events", {
			headers: { Accept: "text/event-stream" },
		});
		expect(response.status()).toBe(401);
		expect(response.headers()["content-type"]).not.toContain("text/event-stream");
		expect((await response.json()).error).toBe("missing authentication");
	});
	test("GET /api/v1/events (SSE) returns 401 for an invalid token", async ({ browser }) => {
		const context = await browser.newContext({ baseURL: APP_URL });
		await context.addCookies([
			{ name: "orcacd_auth", value: "invalid", domain: "127.0.0.1", path: "/" },
		]);
		const response = await context.request.get("/api/v1/events", {
			headers: { Accept: "text/event-stream" },
		});
		expect(response.status()).toBe(401);
		expect((await response.json()).error).toBe("invalid or expired token");
		await context.close();
	});
	test("the browser EventSource fails without a session", async ({ page }) => {
		await page.goto("/api/v1/health");
		const result = await page.evaluate(
			() =>
				new Promise<string>((resolve) => {
					const source = new EventSource("/api/v1/events");
					source.addEventListener("open", () => resolve("open"));
					source.addEventListener("error", () => {
						resolve(source.readyState === EventSource.CLOSED ? "closed" : "reconnecting");
						source.close();
					});
				}),
		);
		expect(result).toBe("closed");
	});
	test("POST /api/v1/applications returns 401", async ({ request }) => {
		expect(
			(
				await request.post("/api/v1/applications", {
					data: { foo: "bar" },
					headers: { Origin: APP_URL },
				})
			).status(),
		).toBe(401);
	});
});
