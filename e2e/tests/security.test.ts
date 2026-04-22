import { test, expect } from "@playwright/test";
import { APP_URL } from "./const.ts";

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
			data: { email: "attacker@example.com", password: "password123456!", name: "Attacker" },
			headers: {
				Origin: APP_URL,
			},
		});
		expect(response.status()).toBe(403);
		const body = await response.json();
		expect(body.error).toBe("registration is disabled");
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
