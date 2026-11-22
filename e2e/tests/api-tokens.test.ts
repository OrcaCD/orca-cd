import { randomUUID } from "node:crypto";
import { test, expect, type Page } from "@playwright/test";
import { api } from "./helpers/api.ts";
import {
	createApiTokenInUi,
	deleteApiTokens,
	listApiTokens,
	tokenClient,
	unknownToken,
} from "./helpers/api-tokens.ts";
import { gotoApp } from "./helpers/navigation.ts";
import { uniqueId } from "./helpers/oidc.ts";
import { expectToast } from "./helpers/users.ts";

const createdTokens: string[] = [];

test.afterEach(async ({ page }) => {
	await deleteApiTokens(page, createdTokens.splice(0));
});

async function createToken(page: Page, scopes: string[]) {
	const name = uniqueId("E2E Token");
	createdTokens.push(name);
	const token = await createApiTokenInUi(page, name, scopes);
	return { name, token };
}

function tokenRow(page: Page, name: string) {
	return page.getByRole("row").filter({ hasText: name });
}

async function revokeInUi(page: Page, name: string) {
	await tokenRow(page, name).getByRole("button", { name: "Revoke" }).click();
	await page.getByRole("dialog").getByRole("button", { name: "Revoke" }).click();
	await expectToast(page, "API token revoked");
	await expect(tokenRow(page, name)).toHaveCount(0);
}

test.describe("API tokens", () => {
	test("are created on the settings page and shown once", async ({ page }) => {
		const { name, token } = await createToken(page, ["application:read", "application:deploy"]);

		const row = tokenRow(page, name);
		await expect(row).toContainText(token.slice(0, 13));
		await expect(row).toContainText("Applications: View");
		await expect(row).toContainText("Applications: Deploy");
		await expect(row).toContainText("Never");
		// Only the prefix is shown or returned again
		await expect(page.getByText(token)).toHaveCount(0);
		expect(JSON.stringify(await listApiTokens(page))).not.toContain(token);
	});

	test("call the API without a session or Origin header", async ({ page }) => {
		const { name, token } = await createToken(page, ["application:read", "application:deploy"]);
		const client = await tokenClient(token);
		try {
			expect((await client.get("/api/v1/applications")).status()).toBe(200);
			// Gets past the Origin check, the token and the permission check
			const deploy = await client.post(`/api/v1/applications/${randomUUID()}/deploy`);
			expect(deploy.status()).toBe(404);
			expect((await deploy.json()).error).toBe("application not found");
		} finally {
			await client.dispose();
		}

		await gotoApp(page, "/settings/api-tokens");
		await expect(tokenRow(page, name)).not.toContainText("Never");
	});

	test("can only do what their scopes allow", async ({ page }) => {
		const { token } = await createToken(page, ["application:read"]);
		const client = await tokenClient(token);
		try {
			const deploy = await client.post(`/api/v1/applications/${randomUUID()}/deploy`);
			expect(deploy.status()).toBe(403);
			expect((await deploy.json()).error).toBe("insufficient permissions");

			// The owner is an admin, but the token has no admin scope
			const admin = await client.get("/api/v1/admin/users");
			expect(admin.status()).toBe(403);
			expect((await admin.json()).error).toBe("admin access required");

			for (const path of ["/auth/profile", "/api-tokens", "/events"]) {
				const response = await client.get(`/api/v1${path}`);
				expect(response.status(), path).toBe(403);
				expect((await response.json()).error).toBe("this route can't be used with an api token");
			}
		} finally {
			await client.dispose();
		}
	});

	test("need the inspect scope to inspect containers", async ({ page }) => {
		const reader = await createToken(page, ["application:read"]);
		const inspector = await createToken(page, ["application:inspect"]);
		await expect(tokenRow(page, inspector.name)).toContainText("Applications: Inspect");

		const applicationId = randomUUID();
		const list = `/api/v1/applications/${applicationId}/containers`;
		const inspect = `${list}/0123456789ab/inspect`;

		const readerClient = await tokenClient(reader.token);
		const inspectorClient = await tokenClient(inspector.token);
		try {
			// Past the permission check, the application doesn't exist
			expect((await readerClient.get(list)).status()).toBe(404);
			const denied = await readerClient.get(inspect);
			expect(denied.status()).toBe(403);
			expect((await denied.json()).error).toBe("insufficient permissions");

			expect((await inspectorClient.get(list)).status()).toBe(404);
			const allowed = await inspectorClient.get(inspect);
			expect(allowed.status()).toBe(404);
			expect((await allowed.json()).error).toBe("application not found");
		} finally {
			await readerClient.dispose();
			await inspectorClient.dispose();
		}
	});

	test("with the admin scope reach the admin routes", async ({ page }) => {
		const { token } = await createToken(page, ["admin"]);
		const client = await tokenClient(token);
		try {
			expect((await client.get("/api/v1/admin/users")).status()).toBe(200);
			// Managing tokens still needs a session
			expect((await client.get("/api/v1/admin/api-tokens")).status()).toBe(403);
		} finally {
			await client.dispose();
		}
	});

	test("an unknown token is rejected even with a session cookie", async ({ page }) => {
		const response = await page.request.get("/api/v1/auth/profile", {
			headers: { Authorization: `Bearer ${unknownToken()}` },
		});
		expect(response.status()).toBe(401);

		const client = await tokenClient(unknownToken());
		try {
			expect((await client.get("/api/v1/applications")).status()).toBe(401);
		} finally {
			await client.dispose();
		}
	});

	test("a revoked token gets 401", async ({ page }) => {
		const { name, token } = await createToken(page, ["application:read"]);
		const client = await tokenClient(token);
		try {
			expect((await client.get("/api/v1/applications")).status()).toBe(200);
			await gotoApp(page, "/settings/api-tokens");
			await revokeInUi(page, name);
			expect((await client.get("/api/v1/applications")).status()).toBe(401);
		} finally {
			await client.dispose();
		}
	});

	test("admins see and revoke them on the admin page", async ({ page }) => {
		const { name, token } = await createToken(page, ["application:read"]);
		const client = await tokenClient(token);
		try {
			expect((await client.get("/api/v1/applications")).status()).toBe(200);
			const [{ id }] = (await listApiTokens(page)).filter((item) => item.name === name);

			// The first use is in the audit log and marked as made with the token
			await expect
				.poll(async () => {
					const { body } = await api(page, "GET", `/admin/audit-logs?search=${id}`);
					return (body.items as { eventType: string; apiTokenId: string | null }[]).map((item) => [
						item.eventType,
						item.apiTokenId,
					]);
				})
				.toContainEqual(["first-used-api-token", id]);
			await gotoApp(page, "/admin/audit-log");
			await page.getByLabel("Search audit log").fill(id);
			// The search is debounced, so the counts wait for the filtered list
			await expect(page.getByText("FIRST-USED-API-TOKEN")).toHaveCount(1);
			await expect(page.getByRole("button", { name: "API token", exact: true })).toHaveCount(1);

			await gotoApp(page, "/admin/api-tokens");
			await expect(tokenRow(page, name)).toContainText("You");
			await revokeInUi(page, name);
			expect((await client.get("/api/v1/applications")).status()).toBe(401);
		} finally {
			await client.dispose();
		}
	});
});
