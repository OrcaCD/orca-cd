import { test, expect, request as pwRequest, type Locator, type Page } from "@playwright/test";
import { APP_URL } from "./const.ts";
import { stopAgent } from "./helpers/agent-container.ts";
import { api, listRepositories, seedRepository, type ApiRepository } from "./helpers/api.ts";
import {
	appContainers,
	BUSYBOX_IMAGE,
	composeProject,
	deployedComposeFile,
	deploymentDirExists,
	inspectContainer,
	pullImage,
	removeApplicationContainers,
} from "./helpers/docker.ts";
import { commit, requireToken } from "./helpers/gitea-mock.ts";
import {
	applicationEvents,
	createOnlineAgent,
	getApplication,
	listApplications,
	newAuthenticatedContext,
	waitForApplication,
	type ApiAgent,
} from "./helpers/setup.ts";

const IMAGE = BUSYBOX_IMAGE;
// Refused immediately by the daemon, so the failing deployment doesn't depend on the network
const UNPULLABLE_IMAGE = "127.0.0.1:1/e2e/missing:1";

const AGENT_NAME = "E2E Deploy Agent";
const REPO = "e2e/deploy-main";
const CASCADE_REPO = "e2e/deploy-cascade";
const APP_NAME = "E2E Deploy App";
const CASCADE_APP_NAME = "E2E Cascade App";
const COMPOSE_PATH = "apps/demo/compose.yaml";

function composeFile(version: number, image = IMAGE) {
	return `services:
  app:
    image: ${image}
    pull_policy: if_not_present
    command: ["sh", "-c", "while true; do sleep 1; done"]
    # PID 1 ignores SIGTERM, which would make every stop wait for the default 10 seconds
    stop_grace_period: 1s
    environment:
      APP_VERSION: "${version}"
    healthcheck:
      test: ["CMD", "true"]
      interval: 2s
      retries: 1
`;
}

test.describe.configure({ mode: "serial", timeout: 120_000 });

let agent: ApiAgent;
let repo: ApiRepository;
let appId = "";
let cascadeAppId = "";
// The compose project the agent derived from the application name, read from its containers
let project = "";
let containerId = "";
let initialCommit: { sha: string; message: string };

test.beforeAll(async ({ browser }) => {
	// Pulling here keeps the registry out of the deploy assertions
	pullImage(IMAGE);

	const context = await newAuthenticatedContext(browser);
	try {
		const page = await context.newPage();
		repo = await seedRepository(
			page,
			REPO,
			{},
			{
				branches: {
					main: { files: { "README.md": "# Deploy\n" } },
					dev: { files: { "README.md": "# Deploy\n" } },
				},
			},
		);
		initialCommit = await commit(REPO, {
			files: { [COMPOSE_PATH]: composeFile(1) },
			message: "Add demo app",
		});
		agent = await createOnlineAgent(page, AGENT_NAME);
	} finally {
		await context.close();
	}
});

test.afterAll(async ({ browser }) => {
	const context = await newAuthenticatedContext(browser);
	try {
		const page = await context.newPage();
		// Deleting through the hub lets the agent clean up, the rest is the fallback
		for (const application of await listApplications(page)) {
			await api(page, "DELETE", `/applications/${application.id}`);
		}
		for (const { id, name } of await listRepositories(page)) {
			if ([REPO, CASCADE_REPO].includes(name)) {
				await api(page, "DELETE", `/repositories/${id}`);
			}
		}
		await api(page, "DELETE", `/agents/${agent.id}`);
	} catch {
		// The containers are removed below either way
	} finally {
		await context.close();
		stopAgent();
		removeApplicationContainers(appId);
		removeApplicationContainers(cascadeAppId);
	}
});

async function selectOption(page: Page, trigger: Locator, label: string) {
	await trigger.click();
	await page.getByRole("option", { name: label, exact: true }).click();
}

async function fillApplicationWizard(page: Page, name: string) {
	await page.goto("/applications");
	await page.getByRole("button", { name: "New Application" }).click();
	const dialog = page.getByRole("dialog");

	await dialog.locator("#name").fill(name);
	await selectOption(page, dialog.locator("#agent-select"), AGENT_NAME);
	await dialog.getByRole("button", { name: "Next" }).click();

	await selectOption(page, dialog.locator("#repository-select"), REPO);
	await selectOption(page, dialog.locator("#branch-select"), "main");
	for (const segment of COMPOSE_PATH.split("/")) {
		await dialog.getByRole("button", { name: segment, exact: true }).click();
	}
	await expect(dialog.locator("#path")).toHaveValue(COMPOSE_PATH);
	await dialog.getByRole("button", { name: "Next" }).click();
	return dialog;
}

function repositoryCard(page: Page) {
	return page.locator("[data-slot='card']", { hasText: REPO });
}

async function syncRepositoryInUi(page: Page) {
	await page.goto("/repositories");
	await repositoryCard(page).getByRole("button", { name: "Card actions" }).click();
	await page.getByRole("menuitem", { name: "Sync" }).click();
	await expect(page.getByText("Sync triggered")).toBeVisible();
}

function runningVersion() {
	const containers = appContainers(appId);
	if (containers.length !== 1) {
		return undefined;
	}
	return inspectContainer(containers[0].id).env.APP_VERSION;
}

async function expectRedeployed(page: Page, version: number, sha: string) {
	await expect.poll(runningVersion, { timeout: 60_000 }).toBe(String(version));
	await waitForApplication(
		page,
		appId,
		(app) => app.commit === sha && app.syncStatus === "synced" && app.healthStatus === "healthy",
	);
}

function eventFor(
	events: Awaited<ReturnType<typeof applicationEvents>>,
	source: string,
	sha: string,
) {
	return events.find((event) => event.source === source && event.commitHash === sha);
}

async function expectEvent(page: Page, source: string, sha: string, status: string) {
	await expect
		.poll(async () => eventFor(await applicationEvents(page, appId), source, sha)?.status, {
			timeout: 30_000,
		})
		.toBe(status);
}

async function updateRepository(page: Page, body: Record<string, unknown>) {
	const response = await api(page, "PUT", `/repositories/${repo.id}`, body);
	expect(response.status).toBe(200);
	return response.body as ApiRepository;
}

test.describe("Deploy an application", () => {
	test("create it in the UI", async ({ page }) => {
		const dialog = await fillApplicationWizard(page, APP_NAME);
		await dialog.getByRole("button", { name: "Add Application" }).click();

		await expect(page.getByText("Application created")).toBeVisible();
		const application = (await listApplications(page)).find((item) => item.name === APP_NAME);
		expect(application).toBeDefined();
		appId = application!.id;
		expect(await getApplication(page, appId)).toMatchObject({
			agentId: agent.id,
			repositoryId: repo.id,
			branch: "main",
			path: COMPOSE_PATH,
			commit: initialCommit.sha,
		});
		await expect(page.getByRole("link", { name: APP_NAME })).toBeVisible();
	});

	test("the agent deploys it to Docker", async ({ page }) => {
		const application = await waitForApplication(
			page,
			appId,
			(app) => app.syncStatus === "synced" && app.healthStatus === "healthy",
		);
		expect(application.commit).toBe(initialCommit.sha);
		expect(application.commitMessage).toBe(initialCommit.message);
		expect(application.lastSyncError).toBeUndefined();
		expect(application.composeFile).toBe(composeFile(1));

		const containers = appContainers(appId);
		expect(containers).toHaveLength(1);
		expect(containers[0].state).toBe("running");
		containerId = containers[0].id;
		const container = inspectContainer(containerId);
		expect(container.image).toBe(IMAGE);
		// The hub can report healthy before Docker finished the first healthcheck
		await expect
			.poll(() => inspectContainer(containerId).health, { timeout: 30_000 })
			.toBe("healthy");
		expect(container.env.APP_VERSION).toBe("1");
		expect(container.labels["orca-cd.application-id"]).toBe(appId);
		expect(container.labels["managed_by"]).toBe("orca-cd");

		project = composeProject(appId)!;
		expect(project).toBe("e2e-deploy-app");
		expect(deployedComposeFile(project)).toBe(composeFile(1));

		await page.goto(`/applications/${appId}/details`);
		await expect(page.getByText("Synced", { exact: true })).toBeVisible();
		await expect(page.getByText("Healthy", { exact: true })).toBeVisible();
		await expect(page.getByText(initialCommit.sha.slice(0, 7))).toBeVisible();
		await expect(page.getByText(initialCommit.message)).toBeVisible();

		const events = await applicationEvents(page, appId);
		expect(events).toHaveLength(1);
		expect(events[0]).toMatchObject({
			type: "deployment",
			source: "application_created",
			status: "succeeded",
		});
		await page.goto(`/applications/${appId}/history`);
		const row = page.getByRole("row").filter({ hasText: "Application created" });
		await expect(row).toHaveCount(1);
		await expect(row).toContainText("Deployment");
		await expect(row).toContainText("Succeeded");
	});

	test("deploy it again manually", async ({ page }) => {
		await page.goto(`/applications/${appId}/details`);
		await page.getByRole("button", { name: "Deploy", exact: true }).click();
		await expect(page.getByText("Deployment started")).toBeVisible();

		await expect
			.poll(
				async () =>
					(await applicationEvents(page, appId)).filter(
						(event) => event.source === "manual" && event.status === "succeeded",
					).length,
				{ timeout: 60_000 },
			)
			.toBe(1);
		await waitForApplication(
			page,
			appId,
			(app) => app.syncStatus === "synced" && app.healthStatus === "healthy",
		);
		expect(appContainers(appId)).toHaveLength(1);
		expect(runningVersion()).toBe("1");
	});

	test("rejects a duplicate name", async ({ page }) => {
		// Names are compared case-insensitively
		const dialog = await fillApplicationWizard(page, APP_NAME.toLowerCase());
		await dialog.getByRole("button", { name: "Add Application" }).click();

		await expect(page.getByText("an application with this name already exists")).toBeVisible();
		await dialog.getByRole("button", { name: "Cancel" }).click();
		expect(await listApplications(page)).toHaveLength(1);
		expect(appContainers(appId)).toHaveLength(1);
	});
});

test.describe("Redeploy on change", () => {
	test("manual repository sync", async ({ page }) => {
		const { sha, message } = await commit(REPO, {
			files: { [COMPOSE_PATH]: composeFile(2) },
			message: "Bump to v2",
		});

		await syncRepositoryInUi(page);

		await expectRedeployed(page, 2, sha);
		const [container] = appContainers(appId);
		expect(container.id).not.toBe(containerId);
		containerId = container.id;
		expect(deployedComposeFile(project)).toBe(composeFile(2));
		await expectEvent(page, "manual", sha, "succeeded");

		await page.goto(`/applications/${appId}/details`);
		await expect(page.getByText(sha.slice(0, 7))).toBeVisible();
		await expect(page.getByText(message)).toBeVisible();
		await page.goto("/repositories");
		await expect(repositoryCard(page).getByText("Success", { exact: true })).toBeVisible();
	});

	test("repository webhook", async ({ page }) => {
		const { webhookSecret } = await updateRepository(page, { syncType: "webhook" });
		expect(webhookSecret).toBeTruthy();
		const before = await applicationEvents(page, appId);
		const { sha } = await commit(REPO, {
			files: { [COMPOSE_PATH]: composeFile(3) },
			message: "Bump to v3",
		});

		const webhook = await pwRequest.newContext({ baseURL: APP_URL });
		const post = (secret: string, data: object) =>
			webhook.post(`/api/v1/webhooks/repositories/${repo.id}`, {
				headers: { Origin: APP_URL, Authorization: `Bearer ${secret}` },
				data,
			});
		try {
			expect((await post("wrong-secret", { branch: "main" })).status()).toBe(401);
			expect((await post(webhookSecret!, { branch: "dev" })).status()).toBe(204);
			expect(runningVersion()).toBe("2");
			expect(await applicationEvents(page, appId)).toHaveLength(before.length);

			expect((await post(webhookSecret!, { branch: "main" })).status()).toBe(204);
		} finally {
			await webhook.dispose();
		}

		await expectRedeployed(page, 3, sha);
		await expectEvent(page, "repository_webhook", sha, "succeeded");
		await updateRepository(page, { syncType: "manual" });
	});

	test("repository polling", async ({ page }) => {
		test.slow();
		await updateRepository(page, { syncType: "polling", pollingIntervalSeconds: 30 });
		try {
			const { sha } = await commit(REPO, {
				files: { [COMPOSE_PATH]: composeFile(4) },
				message: "Bump to v4",
			});

			await expect.poll(runningVersion, { timeout: 120_000 }).toBe("4");
			await expectRedeployed(page, 4, sha);
			await expectEvent(page, "repository_polling", sha, "succeeded");
		} finally {
			await updateRepository(page, { syncType: "manual" });
		}
	});

	test("a failing deployment is reported and a fix recovers", async ({ page }) => {
		const versionBefore = runningVersion();
		const idsBefore = appContainers(appId).map((container) => container.id);
		const broken = await commit(REPO, {
			files: { [COMPOSE_PATH]: composeFile(5, UNPULLABLE_IMAGE) },
			message: "Break the image",
		});

		await syncRepositoryInUi(page);

		const failed = await waitForApplication(
			page,
			appId,
			(app) => app.commit === broken.sha && app.syncStatus === "out_of_sync" && !!app.lastSyncError,
		);
		expect(failed.healthStatus).toBe("unhealthy");
		await expectEvent(page, "manual", broken.sha, "failed");
		const event = eventFor(await applicationEvents(page, appId), "manual", broken.sha);
		expect(event?.errorMessage).toBeTruthy();

		expect(appContainers(appId).map((container) => container.id)).toEqual(idsBefore);
		expect(runningVersion()).toBe(versionBefore);

		await page.goto(`/applications/${appId}/details`);
		await expect(page.getByText("Sync failed")).toBeVisible();
		await expect(page.getByText("Out of Sync", { exact: true })).toBeVisible();
		await expect(page.getByText(broken.sha.slice(0, 7))).toBeVisible();
		await page.goto(`/applications/${appId}/history`);
		const row = page.getByRole("row").filter({ hasText: broken.sha.slice(0, 7) });
		await expect(row).toContainText("Failed");

		const fixed = await commit(REPO, {
			files: { [COMPOSE_PATH]: composeFile(6) },
			message: "Fix the image",
		});
		await syncRepositoryInUi(page);

		await expectRedeployed(page, 6, fixed.sha);
		expect((await getApplication(page, appId)).lastSyncError).toBeUndefined();
		await expectEvent(page, "manual", fixed.sha, "succeeded");
		await page.goto(`/applications/${appId}/details`);
		await expect(page.getByText("Sync failed")).toHaveCount(0);
		await expect(page.getByText("Synced", { exact: true })).toBeVisible();
		await expect(page.getByText("Healthy", { exact: true })).toBeVisible();
		containerId = appContainers(appId)[0].id;
	});

	test("a repository sync failure is shown on the repository", async ({ page }) => {
		const before = await applicationEvents(page, appId);
		await requireToken(REPO, "secret-token");
		try {
			await syncRepositoryInUi(page);

			await expect(repositoryCard(page).getByText("Failed", { exact: true })).toBeVisible();
			const failed = (await api(page, "GET", `/repositories/${repo.id}`)).body as ApiRepository;
			expect(failed.syncStatus).toBe("failed");
			expect(failed.lastSyncError).toMatch(/authentication failed or access denied/i);

			await expect
				.poll(async () => (await applicationEvents(page, appId)).length, { timeout: 30_000 })
				.toBe(before.length + 1);
			const [event] = await applicationEvents(page, appId);
			expect(event).toMatchObject({ type: "commit_sync", source: "manual", status: "failed" });
			expect(event.errorMessage).toMatch(/authentication failed or access denied/i);
		} finally {
			await requireToken(REPO, null);
		}

		await syncRepositoryInUi(page);
		await expect(repositoryCard(page).getByText("Success", { exact: true })).toBeVisible();
		const recovered = (await api(page, "GET", `/repositories/${repo.id}`)).body as ApiRepository;
		expect(recovered.lastSyncError).toBeNull();
	});

	test("an unchanged commit does not recreate the container", async ({ page }) => {
		const idsBefore = appContainers(appId).map((container) => container.id);
		expect(idsBefore).toHaveLength(1);
		const countNoChange = async () =>
			(await applicationEvents(page, appId)).filter((event) => event.status === "no_change").length;
		const before = await countNoChange();

		await syncRepositoryInUi(page);

		await expect.poll(countNoChange, { timeout: 30_000 }).toBe(before + 1);
		expect(appContainers(appId).map((container) => container.id)).toEqual(idsBefore);
		expect(runningVersion()).toBe("6");
	});
});

test.describe("Removal", () => {
	test("delete the application in the UI", async ({ page }) => {
		await page.goto(`/applications/${appId}/settings/general`);
		await page.getByRole("button", { name: "Delete", exact: true }).click();
		await page.getByRole("dialog").getByRole("button", { name: "Proceed" }).click();

		// The hub answers after the agent removed the containers
		await expect(page.getByText(`Application ${APP_NAME} deleted successfully`)).toBeVisible({
			timeout: 30_000,
		});
		await expect(page).toHaveURL("/applications");
		await expect(page.getByRole("link", { name: APP_NAME })).toHaveCount(0);

		expect((await listApplications(page)).find((item) => item.id === appId)).toBeUndefined();
		expect((await api(page, "GET", `/applications/${appId}`)).status).toBe(404);
		expect(appContainers(appId)).toHaveLength(0);
		expect(deploymentDirExists(project)).toBe(false);
	});

	test("deleting the repository deletes its applications", async ({ page }) => {
		const cascadeRepo = await seedRepository(
			page,
			CASCADE_REPO,
			{},
			{ branches: { main: { files: { "compose.yaml": composeFile(1) } } } },
		);
		const created = await api(page, "POST", "/applications", {
			name: CASCADE_APP_NAME,
			repositoryId: cascadeRepo.id,
			agentId: agent.id,
			branch: "main",
			path: "compose.yaml",
		});
		expect(created.status).toBe(201);
		cascadeAppId = created.body.id;
		await waitForApplication(
			page,
			cascadeAppId,
			(app) => app.syncStatus === "synced" && app.healthStatus === "healthy",
		);
		expect(appContainers(cascadeAppId)).toHaveLength(1);

		await page.goto("/applications");
		await expect(page.getByRole("link", { name: CASCADE_APP_NAME })).toBeVisible();
		await page.goto("/repositories");
		await page
			.locator("[data-slot='card']", { hasText: CASCADE_REPO })
			.getByRole("button", { name: "Card actions" })
			.click();
		await page.getByRole("menu").getByRole("button", { name: "Delete" }).click();
		await page.getByRole("dialog").getByRole("button", { name: "Proceed" }).click();
		await expect(page.getByText(`Repository ${CASCADE_REPO} deleted successfully`)).toBeVisible();

		expect((await api(page, "GET", `/applications/${cascadeAppId}`)).status).toBe(404);
		await page.goto("/applications");
		await expect(page.getByRole("link", { name: CASCADE_APP_NAME })).toHaveCount(0);

		removeApplicationContainers(cascadeAppId);
	});
});
