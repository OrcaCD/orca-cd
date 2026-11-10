import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test, expect, type Page } from "@playwright/test";
import { APP_URL } from "./const.ts";
import { api, seedRepository, type ApiRepository } from "./helpers/api.ts";
import { commit, deleteRepo, pushSignedCommit } from "./helpers/gitea-mock.ts";
import { applicationEvents, getApplication, type ApiAgent } from "./helpers/setup.ts";

// Real commits signed with throwaway keys, shared with the backend unit tests
const FIXTURES = join(import.meta.dirname, "../../backend/internal/hub/commitsigning/testdata");
const SIGNED_SHA = "45845b34bb6dff50ff93518db435c7d6944496ef";
const TRUSTED_FINGERPRINT = "SHA256:yIa501Nr65OG7YIn37YygYUHzAEEbJ+qhJLorL9gD8I";

const REPO = "e2e/commit-signing";
const AGENT_NAME = "E2E Signing Agent";
const APP_NAME = "E2E Signing App";
const COMPOSE_PATH = "compose.yaml";
const COMPOSE = "services:\n  web:\n    image: busybox:1.37\n";

const fixture = (name: string) => readFileSync(join(FIXTURES, name), "utf8");

/** Splits a raw commit object into the signed payload and the signature, as Gitea reports them */
function splitSignedCommit(raw: string) {
	let payload = "";
	let signature = "";
	let inSignature = false;
	for (const line of raw.split(/(?<=\n)/)) {
		if (line.startsWith("gpgsig ")) {
			inSignature = true;
			signature += line.slice("gpgsig ".length);
		} else if (inSignature && line.startsWith(" ")) {
			signature += line.slice(1);
		} else {
			inSignature = false;
			payload += line;
		}
	}
	return { payload, signature };
}

test.describe.configure({ mode: "serial" });

let repo: ApiRepository;
let agent: ApiAgent;
let appId: string | undefined;

function createApplication(page: Page) {
	return api(page, "POST", "/applications", {
		name: APP_NAME,
		repositoryId: repo.id,
		agentId: agent.id,
		branch: "main",
		path: COMPOSE_PATH,
	});
}

test.beforeAll(async ({ browser }) => {
	const context = await browser.newContext({ storageState: "playwright/.auth/user.json" });
	const page = await context.newPage();
	repo = await seedRepository(
		page,
		REPO,
		{},
		{ branches: { main: { files: { [COMPOSE_PATH]: COMPOSE } } } },
	);
	// The agent never connects; applications only need it to exist.
	const created = await api(page, "POST", "/agents", { name: AGENT_NAME });
	expect(created.status).toBe(201);
	agent = created.body as ApiAgent;
	await context.close();
});

test.afterAll(async ({ browser }) => {
	const context = await browser.newContext({ storageState: "playwright/.auth/user.json" });
	try {
		const page = await context.newPage();
		await page.goto(`${APP_URL}/repositories`);
		if (appId) {
			await api(page, "DELETE", `/applications/${appId}`);
		}
		await api(page, "DELETE", `/repositories/${repo.id}`);
		await api(page, "DELETE", `/agents/${agent.id}`);
		await deleteRepo(REPO);
	} catch {
		// Leftovers don't affect other tests as they use unique names
	} finally {
		await context.close();
	}
});

test("trusted keys can only be edited while signed commits are required", async ({ page }) => {
	await page.goto(`/repositories/${repo.id}/settings/signing`);
	const newKey = page.getByLabel("New key");
	await expect(newKey).toBeDisabled();

	await page.getByRole("switch", { name: "Require signed commits" }).click();
	await expect(newKey).toBeEnabled();
	await newKey.fill(fixture("ssh_trusted.pub"));
	await page.getByRole("button", { name: "Add key" }).click();
	await expect(page.getByText("Not saved")).toBeVisible();

	await page.getByRole("button", { name: "Update Repository" }).click();
	await expect(page.getByText("Commit signing settings updated")).toBeVisible();
	await expect(page.getByText(TRUSTED_FINGERPRINT)).toBeVisible();
	await expect(page.getByText("Not saved")).toHaveCount(0);

	const { body } = await api(page, "GET", `/repositories/${repo.id}`);
	expect(body).toMatchObject({
		requireSignedCommits: true,
		commitSigningKeys: [
			{ type: "ssh", fingerprint: TRUSTED_FINGERPRINT, identity: "trusted@orcacd.test" },
		],
	});
});

test("an application can't be created from an unsigned commit", async ({ page }) => {
	const created = await createApplication(page);
	expect(created.status).toBe(422);
	expect(created.body.error).toContain("commit is not signed");
});

test("an application is created from a commit signed by a trusted key", async ({ page }) => {
	await pushSignedCommit(REPO, {
		sha: SIGNED_SHA,
		message: "Deploy app",
		files: { [COMPOSE_PATH]: COMPOSE },
		...splitSignedCommit(fixture("ssh_commit.txt")),
	});

	const created = await createApplication(page);
	expect(created.status).toBe(201);
	appId = created.body.id as string;
	expect((await getApplication(page, appId)).commit).toBe(SIGNED_SHA);
});

test("a sync rejects a new unsigned commit", async ({ page }) => {
	const unsigned = await commit(REPO, { files: { [COMPOSE_PATH]: `${COMPOSE}# unsigned\n` } });

	const response = await api(page, "POST", `/repositories/${repo.id}/sync`);
	expect(response.status).toBeLessThan(300);

	await expect
		.poll(
			async () =>
				((await api(page, "GET", `/repositories/${repo.id}`)).body as ApiRepository).lastSyncError,
		)
		.toContain("commit is not signed");
	const events = await applicationEvents(page, appId!);
	expect(events).toContainEqual(
		expect.objectContaining({ status: "failed", commitHash: unsigned.sha }),
	);
	expect((await getApplication(page, appId!)).commit).toBe(SIGNED_SHA);
});
