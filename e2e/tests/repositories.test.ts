import { test, expect, request as pwRequest, type Locator, type Page } from "@playwright/test";
import { APP_URL } from "./const.ts";
import { api, listRepositories, seedRepository as seedHubRepository } from "./helpers/api.ts";
import { createRepo, deleteRepo, repoUrl, requests, requireToken } from "./helpers/gitea-mock.ts";

const MANUAL = "Manual";
const POLLING = "Polling";
const WEBHOOK = "Webhook (Recommended)";

const repoName = (suffix: string) => `repos-${suffix}`;
const fullName = (suffix: string) => `e2e/${repoName(suffix)}`;

test.describe.configure({ mode: "serial" });

const createdIds: string[] = [];

async function findRepository(page: Page, suffix: string) {
	return (await listRepositories(page)).find((repo) => repo.name === fullName(suffix));
}

async function seedRepository(
	page: Page,
	suffix: string,
	extra: Record<string, unknown> = {},
	mock: Parameters<typeof createRepo>[1] = {},
) {
	const repo = await seedHubRepository(page, fullName(suffix), extra, mock);
	createdIds.push(repo.id);
	return repo;
}

function repositoryCard(page: Page, suffix: string) {
	return page.locator("[data-slot='card']", { hasText: fullName(suffix) });
}

async function openWizard(page: Page) {
	await page.goto("/repositories");
	await page.getByRole("button", { name: "Add Repository" }).click();
	const dialog = page.getByRole("dialog");
	await dialog.getByText("Gitea", { exact: true }).click();
	await dialog.getByRole("button", { name: "Next" }).click();
	await expect(dialog.locator("#url")).toBeVisible();
	return dialog;
}

async function submitRepositoryStep(dialog: Locator, url: string, token?: string) {
	await dialog.locator("#url").fill(url);
	await dialog.locator("#authToken").fill(token ?? "");
	await dialog.getByRole("button", { name: "Next" }).click();
}

async function expectStillOnRepositoryStep(dialog: Locator) {
	await expect(dialog.locator("#url")).toBeVisible();
	await expect(dialog.getByRole("button", { name: "Connect Repository" })).toHaveCount(0);
}

async function expectConnectionError(dialog: Locator, message: RegExp) {
	await expect(dialog.getByText("Can't connect to repository")).toBeVisible();
	await expect(dialog.getByText(message)).toBeVisible();
	await expectStillOnRepositoryStep(dialog);
}

async function addRepositoryViaWizard(
	page: Page,
	suffix: string,
	{ syncType, url = repoUrl(fullName(suffix)) }: { syncType: string; url?: string },
) {
	const dialog = await openWizard(page);
	await submitRepositoryStep(dialog, url);
	await dialog.getByText(syncType, { exact: true }).click();
	await dialog.getByRole("button", { name: "Connect Repository" }).click();
	return dialog;
}

async function openCardMenu(page: Page, suffix: string) {
	await page.goto("/repositories");
	await repositoryCard(page, suffix).getByRole("button", { name: "Card actions" }).click();
}

test.afterAll(async ({ browser }) => {
	const context = await browser.newContext({ storageState: "playwright/.auth/user.json" });
	try {
		const page = await context.newPage();
		await page.goto(`${APP_URL}/repositories`);
		for (const id of createdIds) {
			await api(page, "DELETE", `/repositories/${id}`);
		}
	} catch {
		// Leftover repositories don't affect other tests as they use unique names
	} finally {
		await context.close();
	}
});

test.describe("Adding a repository", () => {
	test("with manual sync", async ({ page }) => {
		await createRepo(fullName("add"));

		const dialog = await addRepositoryViaWizard(page, "add", { syncType: MANUAL });
		await expect(dialog.getByText("Repository connected")).toBeVisible();
		await dialog.getByRole("button", { name: "Close", exact: true }).first().click();

		const repo = await findRepository(page, "add");
		expect(repo).toBeDefined();
		createdIds.push(repo!.id);
		expect(repo).toMatchObject({
			provider: "gitea",
			authMethod: "none",
			syncType: "manual",
			appCount: 0,
			lastSyncedAt: null,
		});

		const card = repositoryCard(page, "add");
		await expect(card).toBeVisible();
		await expect(card.getByText("Never")).toBeVisible();

		const received = await requests(fullName("add"));
		expect(received.some((request) => request.path === `/api/v1/repos/${fullName("add")}`)).toBe(
			true,
		);
	});

	test("with webhook sync shows the secret only once", async ({ page }) => {
		await createRepo(fullName("webhook"));

		const dialog = await addRepositoryViaWizard(page, "webhook", { syncType: WEBHOOK });
		await expect(dialog.getByText("Repository connected")).toBeVisible();

		const repo = (await findRepository(page, "webhook"))!;
		createdIds.push(repo.id);
		await expect(
			dialog.getByText(`${APP_URL}/api/v1/webhooks/repositories/${repo.id}`),
		).toBeVisible();
		await expect(dialog.getByText("Webhook Secret")).toBeVisible();
		await expect(dialog.getByText("•".repeat(32))).toBeVisible();

		await page.reload();
		expect(repo.webhookSecret).toBeUndefined();
		const detail = await api(page, "GET", `/repositories/${repo.id}`);
		expect(detail.status).toBe(200);
		expect(detail.body.syncType).toBe("webhook");
		expect(detail.body.webhookSecret).toBeUndefined();
		expect(detail.body.webhookUrl).toBeUndefined();
	});
});

test.describe("Connection validation in the wizard", () => {
	test("rejects text that is not a URL", async ({ page }) => {
		const dialog = await openWizard(page);
		await submitRepositoryStep(dialog, "not-a-url");

		await expect(dialog.getByText("Repository URL must be a valid URL")).toBeVisible();
		await expectStillOnRepositoryStep(dialog);
	});

	test("rejects a URL that does not use https", async ({ page }) => {
		const dialog = await openWizard(page);
		await submitRepositoryStep(dialog, `http://host.docker.internal:3443/${fullName("http")}`);

		await expectConnectionError(dialog, /URL must use https/i);
	});

	test("rejects a repository that does not exist", async ({ page }) => {
		const dialog = await openWizard(page);
		await submitRepositoryStep(dialog, repoUrl(fullName("missing")));

		await expectConnectionError(dialog, /repository not found or access denied/i);
	});

	test("rejects a missing or wrong token and accepts the correct one", async ({ page }) => {
		await createRepo(fullName("token"), { token: "correct-token" });
		const url = repoUrl(fullName("token"));
		const dialog = await openWizard(page);

		await submitRepositoryStep(dialog, url);
		await expectConnectionError(dialog, /authentication failed or access denied/i);

		await submitRepositoryStep(dialog, url, "wrong-token");
		await expectConnectionError(dialog, /authentication failed or access denied/i);

		await submitRepositoryStep(dialog, url, "correct-token");
		await expect(dialog.getByRole("button", { name: "Connect Repository" })).toBeVisible();
		await expect(dialog.getByText("Can't connect to repository")).toHaveCount(0);
	});

	test("rejects a duplicate repository, also with a .git suffix", async ({ page }) => {
		await seedRepository(page, "dup");
		const before = await listRepositories(page);

		for (const suffix of ["", ".git"]) {
			await addRepositoryViaWizard(page, "dup", {
				syncType: MANUAL,
				url: `${repoUrl(fullName("dup"))}${suffix}`,
			});
			await expect(page.getByText("repository already exists")).toBeVisible();
			await page.getByRole("dialog").getByRole("button", { name: "Cancel" }).click();
		}

		expect(await listRepositories(page)).toHaveLength(before.length);
	});
});

test.describe("Repository settings", () => {
	test("update the auth token", async ({ page }) => {
		const repo = await seedRepository(page, "auth");
		await page.goto(`/repositories/${repo.id}/settings/auth`);

		await page.locator("#authToken").fill("new-token");
		await page.getByRole("button", { name: "Update Authentication" }).click();
		await expect(page.getByText("Authentication updated")).toBeVisible();
		expect((await api(page, "GET", `/repositories/${repo.id}`)).body.authMethod).toBe("token");

		await requireToken(fullName("auth"), "right-token");
		await page.locator("#authToken").fill("wrong-token");
		await page.getByRole("button", { name: "Update Authentication" }).click();
		await expect(page.getByText("Can't connect to repository")).toBeVisible();
		await expect(page.getByText(/authentication failed or access denied/i)).toBeVisible();

		// The first toast can still be on screen, which would make the assertion below ambiguous
		await expect(page.getByText("Authentication updated")).toHaveCount(0, { timeout: 10_000 });
		await page.locator("#authToken").fill("right-token");
		await page.getByRole("button", { name: "Update Authentication" }).click();
		await expect(page.getByText("Authentication updated")).toBeVisible();
	});

	test("polling interval limits", async ({ page }) => {
		const repo = await seedRepository(page, "sync");

		const tooLow = await api(page, "PUT", `/repositories/${repo.id}`, {
			syncType: "polling",
			pollingIntervalSeconds: 29,
		});
		expect(tooLow.status).toBe(400);
		expect(tooLow.body.error).toContain("at least 30");
		const atLimit = await api(page, "PUT", `/repositories/${repo.id}`, {
			syncType: "polling",
			pollingIntervalSeconds: 30,
		});
		expect(atLimit.status).toBe(200);
		expect(atLimit.body.pollingIntervalSeconds).toBe(30);
		await api(page, "PUT", `/repositories/${repo.id}`, { syncType: "manual" });

		await page.goto(`/repositories/${repo.id}/settings/sync`);
		await page.getByText(POLLING, { exact: true }).click();
		const interval = page.locator("#pollingIntervalSeconds");
		await interval.fill("45");
		await page.getByRole("button", { name: "Update Repository" }).click();

		expect(
			await interval.evaluate((input: HTMLInputElement) => input.validity.rangeUnderflow),
		).toBe(true);
		await expect(page.getByText("Sync settings updated")).toHaveCount(0);
		expect((await api(page, "GET", `/repositories/${repo.id}`)).body.syncType).toBe("manual");

		await interval.fill("60");
		await page.getByRole("button", { name: "Update Repository" }).click();
		await expect(page.getByText("Sync settings updated")).toBeVisible();
		const updated = (await api(page, "GET", `/repositories/${repo.id}`)).body;
		expect(updated).toMatchObject({ syncType: "polling", pollingIntervalSeconds: 60 });
	});

	test("switching to webhook sync generates a secret", async ({ page }) => {
		const repo = await seedRepository(page, "switch");
		await page.goto(`/repositories/${repo.id}/settings/sync`);

		await page.getByText(WEBHOOK, { exact: true }).click();
		await page.getByRole("button", { name: "Update Repository" }).click();

		await expect(page.getByText("Sync settings updated")).toBeVisible();
		await expect(page.getByText("Webhook Secret")).toBeVisible();
		await expect(
			page.getByText(`${APP_URL}/api/v1/webhooks/repositories/${repo.id}`),
		).toBeVisible();
		expect((await api(page, "GET", `/repositories/${repo.id}`)).body.syncType).toBe("webhook");
	});
});

test.describe("Repository actions", () => {
	test("manual sync without applications", async ({ page }) => {
		const repo = await seedRepository(page, "sync-now");
		await openCardMenu(page, "sync-now");
		const card = repositoryCard(page, "sync-now");
		await expect(card.getByText("Unknown", { exact: true })).toBeVisible();
		await expect(card.getByText("Manual", { exact: true })).toBeVisible();
		await expect(card.getByText("Never")).toBeVisible();

		await page.getByRole("menuitem", { name: "Sync" }).click();

		await expect(page.getByText("Sync triggered")).toBeVisible();
		// The card follows the sync without a reload
		await expect(card.getByText("Success", { exact: true })).toBeVisible();
		await expect(card.getByText("Never")).toHaveCount(0);
		expect((await api(page, "GET", `/repositories/${repo.id}`)).body.lastSyncedAt).not.toBeNull();
		expect(await requests(fullName("sync-now"))).toHaveLength(0);
	});

	test("lists branches across pages and the file tree", async ({ page }) => {
		const branches = Object.fromEntries(
			Array.from({ length: 130 }, (_, i) => [`branch-${String(i).padStart(3, "0")}`, {}]),
		);
		const repo = await seedRepository(
			page,
			"browse",
			{},
			{
				branches: {
					main: {
						files: { "compose.yaml": "services: {}\n", "apps/web/compose.yaml": "services: {}\n" },
					},
					...branches,
				},
			},
		);

		const branchList = await api(page, "GET", `/repositories/${repo.id}/branches`);
		expect(branchList.status).toBe(200);
		expect(branchList.body).toHaveLength(131);
		expect(branchList.body).toContain("main");
		expect(branchList.body).toContain("branch-129");
		const paths = (await requests(fullName("browse"))).map((request) => request.path);
		expect(paths.some((path) => path.includes("/branches?page=2"))).toBe(true);

		const tree = await api(page, "GET", `/repositories/${repo.id}/tree?branch=main`);
		expect(tree.status).toBe(200);
		expect(tree.body).toEqual([
			{ path: "apps", type: "dir" },
			{ path: "apps/web", type: "dir" },
			{ path: "apps/web/compose.yaml", type: "file" },
			{ path: "compose.yaml", type: "file" },
		]);

		expect((await api(page, "GET", `/repositories/${repo.id}/tree`)).status).toBe(400);
		expect((await api(page, "GET", `/repositories/${repo.id}/tree?branch=nope`)).status).toBe(422);
		expect((await api(page, "GET", "/repositories/does-not-exist/branches")).status).toBe(404);
	});

	test("reports an error when the repository disappears", async ({ page }) => {
		const repo = await seedRepository(page, "gone");
		await deleteRepo(fullName("gone"));

		const branches = await api(page, "GET", `/repositories/${repo.id}/branches`);
		expect(branches.status).toBe(422);
		expect(branches.body.error).toMatch(/repository not found or access denied/i);
	});

	test("delete a repository", async ({ page }) => {
		const repo = await seedRepository(page, "delete");
		await openCardMenu(page, "delete");
		await page.getByRole("menu").getByRole("button", { name: "Delete" }).click();
		await page.getByRole("dialog").getByRole("button", { name: "Proceed" }).click();

		await expect(
			page.getByText(`Repository ${fullName("delete")} deleted successfully`),
		).toBeVisible();
		await expect(repositoryCard(page, "delete")).toHaveCount(0);
		expect(await findRepository(page, "delete")).toBeUndefined();
		expect((await api(page, "GET", `/repositories/${repo.id}`)).status).toBe(404);
	});
});

test.describe("Repository list", () => {
	test("search filters the repositories", async ({ page }) => {
		await seedRepository(page, "find-alpha");
		await seedRepository(page, "find-beta");
		await page.goto("/repositories");
		const search = page.getByPlaceholder("Search repositories...");

		await search.fill("find-alpha");
		await expect(repositoryCard(page, "find-alpha")).toBeVisible();
		await expect(repositoryCard(page, "find-beta")).toHaveCount(0);

		await search.fill("no-such-repository");
		await expect(page.getByText("No repositories found")).toBeVisible();

		await search.fill("repos-find-");
		await expect(repositoryCard(page, "find-alpha")).toBeVisible();
		await expect(repositoryCard(page, "find-beta")).toBeVisible();
	});

	test("table layout", async ({ page }) => {
		const repo = await seedRepository(page, "table");
		await seedRepository(page, "other-table");
		await page.goto("/repositories");
		const layoutToggle = page.locator("[data-slot='toggle-group-item']");

		await layoutToggle.nth(1).click();
		const row = page.getByRole("row", { name: fullName("table") });
		await expect(row).toBeVisible();
		await expect(repositoryCard(page, "table")).toHaveCount(0);
		for (const column of ["Name", "Status", "Sync Type", "Last Sync", "Apps"]) {
			await expect(page.getByRole("columnheader", { name: column })).toBeVisible();
		}
		await expect(row.getByText(repoUrl(fullName("table")))).toBeVisible();
		await expect(row.getByText("Manual", { exact: true })).toBeVisible();
		await expect(row.getByText("Never")).toBeVisible();

		// The search applies to the table too
		const search = page.getByPlaceholder("Search repositories...");
		await search.fill(repoName("other-table"));
		await expect(page.getByRole("row", { name: fullName("other-table") })).toBeVisible();
		await expect(row).toHaveCount(0);
		await search.fill("");

		// The layout is remembered
		await page.reload();
		await expect(row).toBeVisible();

		await row.getByRole("button", { name: "Open menu" }).click();
		await page.getByRole("menuitem", { name: "Sync" }).click();
		await expect(page.getByText("Sync triggered")).toBeVisible();
		await expect(row.getByText("Success", { exact: true })).toBeVisible();
		expect((await api(page, "GET", `/repositories/${repo.id}`)).body.syncStatus).toBe("success");

		await layoutToggle.nth(0).click();
		await expect(repositoryCard(page, "table")).toBeVisible();
	});
});

test.describe("Repository settings navigation", () => {
	test("move between the settings pages", async ({ page }) => {
		const repo = await seedRepository(page, "nav");
		const breadcrumb = page.getByRole("navigation", { name: "Breadcrumb" });

		await openCardMenu(page, "nav");
		await page.getByRole("menuitem", { name: "Settings" }).click();
		await expect(page).toHaveURL(`/repositories/${repo.id}/settings/auth`);
		await expect(page.getByText("Edit Authentication")).toBeVisible();
		await expect(breadcrumb.getByText(fullName("nav"))).toBeVisible();
		await expect(breadcrumb.getByText("Auth", { exact: true })).toBeVisible();

		await page.getByRole("link", { name: "Edit Sync" }).click();
		await expect(page).toHaveURL(`/repositories/${repo.id}/settings/sync`);
		await expect(page.getByText("Edit Synchronization")).toBeVisible();
		await expect(breadcrumb.getByText("Edit Sync", { exact: true })).toBeVisible();

		await page.getByRole("link", { name: "Auth", exact: true }).click();
		await expect(page).toHaveURL(`/repositories/${repo.id}/settings/auth`);
		await expect(page.getByText("Edit Authentication")).toBeVisible();

		await breadcrumb.getByRole("link", { name: "Repositories" }).click();
		await expect(page).toHaveURL("/repositories");
		await expect(repositoryCard(page, "nav")).toBeVisible();
	});
});

test.describe("Live updates", () => {
	test("the list follows changes without a reload", async ({ page }) => {
		await page.goto("/repositories");
		await expect(page.getByText("Loading repositories...")).toHaveCount(0);
		const card = repositoryCard(page, "live");
		await expect(card).toHaveCount(0);

		// Created elsewhere, e.g. by another user
		const repo = await seedRepository(page, "live");
		await expect(card).toBeVisible();
		await expect(card.getByText("Unknown", { exact: true })).toBeVisible();

		const sync = await api(page, "POST", `/repositories/${repo.id}/sync`);
		expect(sync.status).toBe(202);
		await expect(card.getByText("Success", { exact: true })).toBeVisible();

		await api(page, "PUT", `/repositories/${repo.id}`, {
			syncType: "polling",
			pollingIntervalSeconds: 3600,
		});
		await expect(card.getByText("Polling", { exact: true })).toBeVisible();

		await api(page, "DELETE", `/repositories/${repo.id}`);
		await expect(card).toHaveCount(0);
	});
});

test.describe("Repository webhook endpoint", () => {
	test("rejects unauthorized and invalid calls", async ({ page }) => {
		const manual = await seedRepository(page, "hook-manual");
		const webhook = await seedRepository(page, "hook-secret", { syncType: "webhook" });

		const anonymous = await pwRequest.newContext({ baseURL: APP_URL });
		const headers = { Origin: APP_URL };

		const notConfigured = await anonymous.post(`/api/v1/webhooks/repositories/${manual.id}`, {
			headers,
		});
		expect(notConfigured.status()).toBe(400);
		expect((await notConfigured.json()).error).toContain("not configured for webhook sync");

		const wrongSecret = await anonymous.post(`/api/v1/webhooks/repositories/${webhook.id}`, {
			headers: { ...headers, Authorization: "Bearer wrong-secret" },
		});
		expect(wrongSecret.status()).toBe(401);

		const missingSecret = await anonymous.post(`/api/v1/webhooks/repositories/${webhook.id}`, {
			headers,
		});
		expect(missingSecret.status()).toBe(401);

		const unknown = await anonymous.post("/api/v1/webhooks/repositories/does-not-exist", {
			headers,
		});
		expect(unknown.status()).toBe(404);

		await anonymous.dispose();
	});
});
