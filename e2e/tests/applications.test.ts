import { test, expect, request as pwRequest, type Locator, type Page } from "@playwright/test";
import { APP_URL, HUB_WS_URL } from "./const.ts";
import { startAgent, stopAgent } from "./helpers/agent-container.ts";
import { api, listRepositories, seedRepository, type ApiRepository } from "./helpers/api.ts";
import {
	appContainers,
	BUSYBOX_IMAGE,
	composeProject,
	containerMounts,
	deployedComposeFile,
	deploymentDirExists,
	execInContainer,
	inspectContainer,
	projectContainers,
	pullImage,
	removeApplicationContainers,
	removeVolumes,
	startContainer,
	stopContainer,
} from "./helpers/docker.ts";
import { commit, requireToken } from "./helpers/gitea-mock.ts";
import { gotoApp } from "./helpers/navigation.ts";
import {
	createNotification,
	deleteNotifications,
	getNotification,
	type ApiNotification,
} from "./helpers/notifications.ts";
import {
	clearRequests,
	hookUrl,
	receivedRequests,
	setStatus,
	waitForMessages,
} from "./helpers/receiver-mock.ts";
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
// The healthcheck passes while this file exists, so tests can fail it with docker exec
const HEALTHY_FILE = "/tmp/healthy";

const AGENT_NAME = "E2E Deploy Agent";
const REPO = "e2e/deploy-main";
const CASCADE_REPO = "e2e/deploy-cascade";
const OFFLINE_REPO = "e2e/deploy-offline";
const MOVE_REPO = "e2e/deploy-move";
const APP_NAME = "E2E Deploy App";
const CASCADE_APP_NAME = "E2E Cascade App";
const OFFLINE_APP_NAME = "E2E Offline App";
const MOVE_APP_NAME = "E2E Move App";
const RENAMED_APP_NAME = "E2E Moved App";
const MOVE_TARGET_AGENT_NAME = "E2E Move Target Agent";
const DEPLOY_PROMPT = "These changes affect the deployment. Deploy now to apply them.";
const COMPOSE_PATH = "apps/demo/compose.yaml";

const NOTIFY_REPO = "e2e/deploy-notify";
const NOTIFY_APP_NAME = "E2E Notify Failing App";
const SUCCESS_HOOK = "deploy-success";
const FAILURE_HOOK = "deploy-failure";
const SCOPED_HOOK = "deploy-scoped";
const DISABLED_HOOK = "deploy-disabled";
const NOTIFY_HOOKS = [SUCCESS_HOOK, FAILURE_HOOK, SCOPED_HOOK, DISABLED_HOOK];
const DEPLOYMENT_EVENTS = ["application.deployment.succeeded", "application.deployment.failed"];

const POLICY_REPO = "e2e/deploy-policy";
const POLICY_ERROR = "deployment rejected by security policy";
const IDLE = "while true; do sleep 1; done";

function policyComposeFile(serviceConfig: string, extra = "", script = IDLE) {
	return `services:
  app:
    image: ${IMAGE}
    pull_policy: if_not_present
    command: ["sh", "-c", "${script}"]
    stop_grace_period: 1s
${serviceConfig}${extra}`;
}

const rejectedCases = [
	{
		title: "privileged mode",
		name: "E2E Policy Privileged",
		project: "e2e-policy-privileged",
		compose: policyComposeFile("    privileged: true\n"),
		violation: 'service "app": privileged mode is not allowed',
	},
	{
		title: "a dangerous capability",
		name: "E2E Policy Capability",
		project: "e2e-policy-capability",
		compose: policyComposeFile("    cap_add:\n      - SYS_ADMIN\n"),
		violation: 'service "app": cap_add "SYS_ADMIN" is not allowed',
	},
	{
		title: "a Docker socket bind mount",
		name: "E2E Policy Socket",
		project: "e2e-policy-socket",
		compose: policyComposeFile("    volumes:\n      - /var/run/docker.sock:/var/run/docker.sock\n"),
		violation: 'bind mount source "/var/run/docker.sock"',
	},
	{
		title: "a sensitive host path bind mount",
		name: "E2E Policy Host Path",
		project: "e2e-policy-host-path",
		compose: policyComposeFile("    volumes:\n      - /etc:/host-etc:ro\n"),
		violation: 'bind mount source "/etc" is under sensitive host path "/etc"',
	},
	{
		title: "a disallowed security option",
		name: "E2E Policy Security Opt",
		project: "e2e-policy-security-opt",
		compose: policyComposeFile("    security_opt:\n      - seccomp=unconfined\n"),
		violation: 'service "app": security_opt "seccomp=unconfined" is not allowed',
	},
];

const SAFE_NAME = "E2E Policy Safe";
const SAFE_PROJECT = "e2e-policy-safe";
const SAFE_VOLUME = `${SAFE_PROJECT}_data`;
const safeCompose = policyComposeFile(
	`    volumes:
      - data:/named
      - ./bound:/bound
`,
	`volumes:
  data:
`,
	`echo named > /named/file; echo bind > /bound/file; ${IDLE}`,
);

const policyPath = (name: string) => `${name.toLowerCase().replaceAll(" ", "-")}/compose.yaml`;

function composeFile(version: number, image = IMAGE) {
	return `services:
  app:
    image: ${image}
    pull_policy: if_not_present
    command: ["sh", "-c", "touch ${HEALTHY_FILE}; while true; do sleep 1; done"]
    # PID 1 ignores SIGTERM, which would make every stop wait for the default 10 seconds
    stop_grace_period: 1s
    environment:
      APP_VERSION: "${version}"
    healthcheck:
      test: ["CMD", "test", "-f", "${HEALTHY_FILE}"]
      interval: 2s
      retries: 1
`;
}

test.describe.configure({ mode: "serial", timeout: 120_000 });

let agent: ApiAgent;
let repo: ApiRepository;
let appId = "";
let cascadeAppId = "";
let offlineAppId = "";
let moveAppId = "";
let moveTargetAgent: ApiAgent | undefined;
let policyRepo: ApiRepository | undefined;
const policyAppIds = new Map<string, string>();
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
		// Never connected, so it can't undeploy, deleting it removes its applications
		if (moveTargetAgent) {
			await api(page, "DELETE", `/agents/${moveTargetAgent.id}`);
		}
		// Deleting through the hub lets the agent clean up, the rest is the fallback
		for (const application of await listApplications(page)) {
			await api(page, "DELETE", `/applications/${application.id}`);
		}
		for (const { id, name } of await listRepositories(page)) {
			if ([REPO, CASCADE_REPO, OFFLINE_REPO, MOVE_REPO, POLICY_REPO, NOTIFY_REPO].includes(name)) {
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
		removeApplicationContainers(offlineAppId);
		removeApplicationContainers(moveAppId);
		policyAppIds.forEach(removeApplicationContainers);
		removeVolumes(SAFE_VOLUME);
	}
});

async function selectOption(page: Page, trigger: Locator, label: string) {
	await trigger.click();
	await page.getByRole("option", { name: label, exact: true }).click();
}

async function fillApplicationWizard(page: Page, name: string) {
	await gotoApp(page, "/applications");
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

/** The application's card in the list, not the overview's last deployment link. */
function applicationListEntry(page: Page, name: string) {
	return page.getByRole("link", { name }).filter({ has: page.locator("[data-slot='card']") });
}

function repositoryCard(page: Page) {
	return page.locator("[data-slot='card']", { hasText: REPO });
}

async function syncRepositoryInUi(page: Page) {
	await gotoApp(page, "/repositories");
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

async function getAgent(page: Page) {
	const { body } = await api(page, "GET", `/agents/${agent.id}`);
	return body as ApiAgent;
}

/** Creates a repository with its own application and waits until it runs healthy. */
async function deployInSeparateRepository(page: Page, repoName: string, name: string) {
	const separateRepo = await seedRepository(
		page,
		repoName,
		{},
		{ branches: { main: { files: { "compose.yaml": composeFile(1) } } } },
	);
	const created = await api(page, "POST", "/applications", {
		name,
		repositoryId: separateRepo.id,
		agentId: agent.id,
		branch: "main",
		path: "compose.yaml",
	});
	expect(created.status).toBe(201);
	const id = created.body.id as string;
	await waitForApplication(
		page,
		id,
		(app) => app.syncStatus === "synced" && app.healthStatus === "healthy",
	);
	expect(appContainers(id)).toHaveLength(1);
	return { repo: separateRepo, appId: id };
}

async function deleteRepositoryInUi(page: Page, repoName: string) {
	await gotoApp(page, "/repositories");
	await page
		.locator("[data-slot='card']", { hasText: repoName })
		.getByRole("button", { name: "Card actions" })
		.click();
	await page.getByRole("menu").getByRole("button", { name: "Delete" }).click();
	await page.getByRole("dialog").getByRole("button", { name: "Proceed" }).click();
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
		await expect(applicationListEntry(page, APP_NAME)).toBeVisible();
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

		await gotoApp(page, `/applications/${appId}/details`);
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
		await gotoApp(page, `/applications/${appId}/history`);
		const item = page.getByRole("listitem").filter({ hasText: "Application created" });
		await expect(item).toHaveCount(1);
		await expect(item).toContainText("Deployment");
		await expect(item).toContainText("Succeeded");

		await page.getByRole("button", { name: "Table view" }).click();
		const row = page.getByRole("row").filter({ hasText: "Application created" });
		await expect(row).toHaveCount(1);
		await expect(row).toContainText("Deployment");
		await expect(row).toContainText("Succeeded");
	});

	test("deploy it again manually", async ({ page }) => {
		await gotoApp(page, `/applications/${appId}/details`);
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

	test("pull images from the application details", async ({ page }) => {
		await page.goto(`/applications/${appId}/details`);
		await page.getByRole("button", { name: "More deploy options" }).click();
		await page.getByRole("menuitem", { name: /Pull images/ }).click();
		await expect(page.getByText("Image pull started")).toBeVisible();

		// The image is already up to date, so the pull finishes without a redeploy.
		await expect
			.poll(
				async () =>
					(await applicationEvents(page, appId)).find(
						(event) => event.type === "image_update" && event.source === "manual",
					)?.status,
				{ timeout: 30_000 },
			)
			.toBe("no_change");
		await waitForApplication(
			page,
			appId,
			(app) => app.syncStatus === "synced" && app.healthStatus === "healthy",
		);
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

		await gotoApp(page, `/applications/${appId}/details`);
		await expect(page.getByText(sha.slice(0, 7))).toBeVisible();
		await expect(page.getByText(message)).toBeVisible();
		await gotoApp(page, "/repositories");
		await expect(repositoryCard(page).getByText("Success", { exact: true })).toBeVisible();
	});

	test("repository sync from the application details", async ({ page }) => {
		const { sha, message } = await commit(REPO, {
			files: { "README.md": "# Demo\n" },
			message: "Add readme",
		});

		await gotoApp(page, `/applications/${appId}/details`);
		await page.getByRole("button", { name: "More deploy options" }).click();
		await page.getByRole("menuitem", { name: /Sync repository/ }).click();
		await expect(page.getByText("Sync triggered")).toBeVisible();

		await expectEvent(page, "manual", sha, "no_change");
		await waitForApplication(page, appId, (app) => app.commit === sha);
		await expect(page.getByText(sha.slice(0, 7))).toBeVisible();
		await expect(page.getByText(message)).toBeVisible();
		expect(appContainers(appId).map((container) => container.id)).toEqual([containerId]);
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
		expect(failed.syncStatus).toBe("out_of_sync");
		// Health follows the containers, and the previous version keeps running
		await waitForApplication(page, appId, (app) => app.healthStatus === "healthy");
		await expectEvent(page, "manual", broken.sha, "failed");
		const event = eventFor(await applicationEvents(page, appId), "manual", broken.sha);
		expect(event?.errorMessage).toBeTruthy();

		expect(appContainers(appId).map((container) => container.id)).toEqual(idsBefore);
		expect(runningVersion()).toBe(versionBefore);

		await gotoApp(page, `/applications/${appId}/details`);
		await expect(page.getByText("Sync failed")).toBeVisible();
		await expect(page.getByText("Out of Sync", { exact: true })).toBeVisible();
		await expect(page.getByText(broken.sha.slice(0, 7))).toBeVisible();
		await gotoApp(page, `/applications/${appId}/history`);
		const item = page.getByRole("listitem").filter({ hasText: broken.sha.slice(0, 7) });
		await expect(item).toContainText("Failed");

		const fixed = await commit(REPO, {
			files: { [COMPOSE_PATH]: composeFile(6) },
			message: "Fix the image",
		});
		await syncRepositoryInUi(page);

		await expectRedeployed(page, 6, fixed.sha);
		expect((await getApplication(page, appId)).lastSyncError).toBeUndefined();
		await expectEvent(page, "manual", fixed.sha, "succeeded");
		await gotoApp(page, `/applications/${appId}/details`);
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

test.describe("Runtime health", () => {
	const healthBadge = (page: Page, label: string) => page.getByText(label, { exact: true });

	test("a failing healthcheck marks the application unhealthy", async ({ page }) => {
		const [container] = appContainers(appId);
		expect(container.state).toBe("running");
		await waitForApplication(page, appId, (app) => app.healthStatus === "healthy");

		// The page stays open, so the status change has to arrive through live updates
		await gotoApp(page, `/applications/${appId}/details`);
		await expect(healthBadge(page, "Healthy")).toBeVisible();

		execInContainer(container.id, ["rm", HEALTHY_FILE]);
		try {
			await expect
				.poll(() => inspectContainer(container.id).health, { timeout: 30_000 })
				.toBe("unhealthy");

			await expect(healthBadge(page, "Unhealthy")).toBeVisible({ timeout: 30_000 });
			await expect(healthBadge(page, "Healthy")).toHaveCount(0);
			const unhealthy = await getApplication(page, appId);
			expect(unhealthy.healthStatus).toBe("unhealthy");
			// A runtime health change must not touch the sync state
			expect(unhealthy.syncStatus).toBe("synced");
			expect(unhealthy.lastSyncError).toBeUndefined();

			await gotoApp(page, "/applications");
			await expect(
				page.getByRole("link", { name: APP_NAME }).getByText("Unhealthy", { exact: true }),
			).toBeVisible();
		} finally {
			execInContainer(container.id, ["touch", HEALTHY_FILE]);
		}
	});

	test("the application recovers once the healthcheck passes again", async ({ page }) => {
		const [container] = appContainers(appId);
		await gotoApp(page, `/applications/${appId}/details`);
		// The previous test restored the file, but the hub may not have processed it yet
		await waitForApplication(page, appId, (app) => app.healthStatus === "healthy");
		await expect(healthBadge(page, "Healthy")).toBeVisible();

		execInContainer(container.id, ["rm", HEALTHY_FILE]);
		try {
			await expect(healthBadge(page, "Unhealthy")).toBeVisible({ timeout: 30_000 });
		} finally {
			execInContainer(container.id, ["touch", HEALTHY_FILE]);
		}

		await expect(healthBadge(page, "Healthy")).toBeVisible({ timeout: 30_000 });
		await expect(healthBadge(page, "Unhealthy")).toHaveCount(0);
		const recovered = await waitForApplication(
			page,
			appId,
			(app) => app.healthStatus === "healthy",
		);
		expect(recovered.syncStatus).toBe("synced");
		expect(appContainers(appId).map((item) => item.id)).toEqual([container.id]);
	});

	test("a stopped container marks the application stopped", async ({ page }) => {
		const [container] = appContainers(appId);
		await page.goto(`/applications/${appId}/details`);
		await waitForApplication(page, appId, (app) => app.healthStatus === "healthy");
		await expect(healthBadge(page, "Healthy")).toBeVisible();

		stopContainer(container.id);
		try {
			await expect(healthBadge(page, "Stopped")).toBeVisible({ timeout: 30_000 });
			await expect(healthBadge(page, "Healthy")).toHaveCount(0);
			const stopped = await getApplication(page, appId);
			expect(stopped.healthStatus).toBe("stopped");
			expect(stopped.syncStatus).toBe("synced");
		} finally {
			startContainer(container.id);
		}

		await expect(healthBadge(page, "Healthy")).toBeVisible({ timeout: 30_000 });
		await expect(healthBadge(page, "Stopped")).toHaveCount(0);
	});
});

test.describe("Move and rename", () => {
	const deployPrompt = (page: Page) =>
		page.locator("[data-slot='toast']", { hasText: DEPLOY_PROMPT });

	async function openGeneralSettings(page: Page, name: string) {
		await page.goto(`/applications/${moveAppId}/settings/general`);
		// The form is filled once the application has loaded
		await expect(page.locator("#name")).toHaveValue(name);
	}

	test("renaming the application removes the old compose project", async ({ page }) => {
		({ appId: moveAppId } = await deployInSeparateRepository(page, MOVE_REPO, MOVE_APP_NAME));
		const oldProject = composeProject(moveAppId)!;
		expect(oldProject).toBe("e2e-move-app");
		expect(deploymentDirExists(oldProject)).toBe(true);

		await openGeneralSettings(page, MOVE_APP_NAME);
		await page.locator("#name").fill(RENAMED_APP_NAME);
		await page.getByRole("button", { name: "Save Changes" }).click();

		// The hub answers after the agent removed the old project
		await expect(deployPrompt(page)).toBeVisible({ timeout: 30_000 });
		expect(appContainers(moveAppId)).toHaveLength(0);
		expect(deploymentDirExists(oldProject)).toBe(false);

		await deployPrompt(page).getByRole("button", { name: "Deploy" }).click();
		await expect(page.getByText("Deployment started")).toBeVisible();
		await waitForApplication(
			page,
			moveAppId,
			(app) =>
				app.name === RENAMED_APP_NAME &&
				app.syncStatus === "synced" &&
				app.healthStatus === "healthy",
		);
		await expect.poll(() => appContainers(moveAppId).length, { timeout: 30_000 }).toBe(1);
		const newProject = composeProject(moveAppId)!;
		expect(newProject).toBe("e2e-moved-app");
		expect(deploymentDirExists(newProject)).toBe(true);
		expect(deploymentDirExists(oldProject)).toBe(false);
	});

	test("moving the application to another agent removes it from the old one", async ({ page }) => {
		const created = await api(page, "POST", "/agents", { name: MOVE_TARGET_AGENT_NAME });
		expect(created.status).toBe(201);
		moveTargetAgent = created.body as ApiAgent;
		const project = composeProject(moveAppId)!;

		await openGeneralSettings(page, RENAMED_APP_NAME);
		await selectOption(page, page.locator("#agent-select"), MOVE_TARGET_AGENT_NAME);
		await page.getByRole("button", { name: "Save Changes" }).click();

		await expect(deployPrompt(page)).toBeVisible({ timeout: 30_000 });
		expect((await getApplication(page, moveAppId)).agentId).toBe(moveTargetAgent.id);
		expect(appContainers(moveAppId)).toHaveLength(0);
		expect(deploymentDirExists(project)).toBe(false);

		// The target agent never connected, so it can't undeploy and moving back is blocked
		await openGeneralSettings(page, RENAMED_APP_NAME);
		await selectOption(page, page.locator("#agent-select"), AGENT_NAME);
		await page.getByRole("button", { name: "Save Changes" }).click();
		await expect(page.getByText(/agent is offline; cannot move or rename application/)).toBeVisible(
			{
				timeout: 30_000,
			},
		);
		expect((await getApplication(page, moveAppId)).agentId).toBe(moveTargetAgent.id);

		// Deleting the agent removes its applications
		expect((await api(page, "DELETE", `/agents/${moveTargetAgent.id}`)).status).toBe(200);
		moveTargetAgent = undefined;
		expect((await api(page, "GET", `/applications/${moveAppId}`)).status).toBe(404);
	});
});

test.describe("Compose security policy", () => {
	test.beforeAll(async ({ browser }) => {
		const context = await newAuthenticatedContext(browser);
		try {
			const page = await context.newPage();
			const files = Object.fromEntries([
				...rejectedCases.map(({ name, compose }) => [policyPath(name), compose]),
				[policyPath(SAFE_NAME), safeCompose],
			]);
			policyRepo = await seedRepository(page, POLICY_REPO, {}, { branches: { main: { files } } });
		} finally {
			await context.close();
		}
	});

	async function createPolicyApplication(page: Page, name: string) {
		const created = await api(page, "POST", "/applications", {
			name,
			repositoryId: policyRepo!.id,
			agentId: agent.id,
			branch: "main",
			path: policyPath(name),
		});
		expect(created.status).toBe(201);
		const id = created.body.id as string;
		policyAppIds.set(name, id);
		return id;
	}

	for (const { title, name, project, violation } of rejectedCases) {
		test(`rejects ${title}`, async ({ page }) => {
			const id = await createPolicyApplication(page, name);

			const failed = await waitForApplication(
				page,
				id,
				(app) => app.syncStatus === "out_of_sync" && !!app.lastSyncError,
			);
			expect(failed.lastSyncError).toContain(POLICY_ERROR);
			expect(failed.lastSyncError).toContain(violation);
			expect(failed.lastSyncedAt).toBeNull();

			await expect
				.poll(async () => (await applicationEvents(page, id)).map((event) => event.status), {
					timeout: 30_000,
				})
				.toEqual(["failed"]);
			const [event] = await applicationEvents(page, id);
			expect(event).toMatchObject({ type: "deployment", source: "application_created" });
			expect(event.errorMessage).toContain(POLICY_ERROR);
			expect(event.errorMessage).toContain(violation);

			// The agent labels containers only after the policy ran, so look at the compose project
			expect(projectContainers(project)).toHaveLength(0);
			expect(appContainers(id)).toHaveLength(0);
		});
	}

	test("shows the rejection on the application", async ({ page }) => {
		const [{ name, violation }] = rejectedCases;
		const id = policyAppIds.get(name)!;

		await gotoApp(page, `/applications/${id}/details`);
		await expect(page.getByText("Sync failed")).toBeVisible();
		await expect(page.getByText("Out of Sync", { exact: true })).toBeVisible();
		await expect(page.getByText(violation, { exact: false })).toBeVisible();

		await gotoApp(page, `/applications/${id}/history`);
		const item = page.getByRole("listitem").filter({ hasText: "Application created" });
		await expect(item).toHaveCount(1);
		await expect(item).toContainText("Failed");
	});

	test("a named volume and a bind mount inside the deployment still deploy", async ({ page }) => {
		const id = await createPolicyApplication(page, SAFE_NAME);

		// Settling on any result makes a rejection show up as the error instead of a timeout
		const deployed = await waitForApplication(
			page,
			id,
			(app) => app.syncStatus === "synced" || !!app.lastSyncError,
		);
		expect(deployed.lastSyncError).toBeUndefined();
		expect(deployed.syncStatus).toBe("synced");
		await expect.poll(() => appContainers(id).length, { timeout: 30_000 }).toBe(1);
		const [container] = appContainers(id);
		expect(container.state).toBe("running");
		expect(composeProject(id)).toBe(SAFE_PROJECT);

		const mounts = containerMounts(container.id);
		expect(mounts).toHaveLength(2);
		expect(mounts.find((mount) => mount.Destination === "/named")).toMatchObject({
			Type: "volume",
			Name: SAFE_VOLUME,
		});
		const bind = mounts.find((mount) => mount.Destination === "/bound");
		expect(bind?.Type).toBe("bind");
		// The agent translates the path inside its container to the one on the Docker host
		expect(bind?.Source).toMatch(new RegExp(`/${SAFE_PROJECT}/bound$`));

		await expect
			.poll(() => execInContainer(container.id, ["cat", "/named/file"]), { timeout: 15_000 })
			.toBe("named");
		expect(execInContainer(container.id, ["cat", "/bound/file"])).toBe("bind");

		const events = await applicationEvents(page, id);
		expect(events).toHaveLength(1);
		expect(events[0]).toMatchObject({ type: "deployment", status: "succeeded" });
	});
});

test.describe("Deployment notifications", () => {
	let notifications: Record<string, ApiNotification> = {};
	let failingAppId = "";

	test.beforeAll(async () => {
		for (const hook of NOTIFY_HOOKS) {
			await setStatus(hook, 200);
		}
	});

	test.afterAll(async ({ browser }) => {
		const context = await newAuthenticatedContext(browser);
		try {
			const page = await context.newPage();
			await deleteNotifications(
				page,
				Object.values(notifications).map((notification) => notification.id),
			);
		} catch {
			// The notifications are only assigned to applications that are removed anyway
		} finally {
			await context.close();
			removeApplicationContainers(failingAppId);
		}
	});

	async function deployManually(page: Page) {
		const succeeded = async () =>
			(await applicationEvents(page, appId)).filter(
				(event) => event.source === "manual" && event.status === "succeeded",
			).length;
		const before = await succeeded();
		const response = await api(page, "POST", `/applications/${appId}/deploy`);
		expect(response.status).toBe(202);
		await expect.poll(succeeded, { timeout: 60_000 }).toBe(before + 1);
		await waitForApplication(page, appId, (app) => app.syncStatus === "synced");
	}

	async function expectNoMessages(...hooks: string[]) {
		// Notifications are sent one after another, so the others had their chance by now
		for (const hook of hooks) {
			expect(await receivedRequests(hook), hook).toHaveLength(0);
		}
	}

	test("creates the notifications", async ({ page }) => {
		const webhook = (name: string, hook: string, extra: Record<string, unknown>) =>
			createNotification(page, {
				name,
				type: "webhook",
				config: JSON.stringify({ webhookUrl: hookUrl(hook) }),
				...extra,
			});

		notifications = {
			[SUCCESS_HOOK]: await webhook("E2E Deploy Success", SUCCESS_HOOK, {
				events: ["application.deployment.succeeded"],
				allApplications: true,
			}),
			[FAILURE_HOOK]: await webhook("E2E Deploy Failure", FAILURE_HOOK, {
				events: ["application.deployment.failed"],
				allApplications: true,
			}),
			[SCOPED_HOOK]: await createNotification(page, {
				name: "E2E Deploy Scoped",
				type: "custom",
				config: `generic://host.docker.internal:${new URL(hookUrl(SCOPED_HOOK)).port}/hooks/${SCOPED_HOOK}?disabletls=yes&template=json`,
				events: DEPLOYMENT_EVENTS,
				applicationIds: [appId],
			}),
			[DISABLED_HOOK]: await webhook("E2E Deploy Disabled", DISABLED_HOOK, {
				events: DEPLOYMENT_EVENTS,
				allApplications: true,
				enabled: false,
			}),
		};
		expect(notifications[SCOPED_HOOK].applicationIds).toEqual([appId]);

		// Creating verifies the config with a test message, even for a disabled notification
		for (const hook of NOTIFY_HOOKS) {
			expect(await receivedRequests(hook), hook).toHaveLength(1);
			await clearRequests(hook);
		}
	});

	test("a successful deployment is sent to the subscribed notifications", async ({ page }) => {
		await deployManually(page);

		const message = `Success: deployment succeeded for ${APP_NAME}`;
		expect(await waitForMessages(SUCCESS_HOOK, 1)).toEqual([message]);
		expect(await waitForMessages(SCOPED_HOOK, 1)).toEqual([message]);
		const [request] = await receivedRequests(SUCCESS_HOOK);
		expect(request.method).toBe("POST");
		expect(request.headers["content-type"]).toContain("application/json");

		// Not subscribed to the event, or disabled
		await expectNoMessages(FAILURE_HOOK, DISABLED_HOOK);
		expect((await getNotification(page, "E2E Deploy Success"))!.status).toBe("success");
	});

	test("a failed deployment is sent to the subscribed notifications", async ({ page }) => {
		for (const hook of NOTIFY_HOOKS) {
			await clearRequests(hook);
		}
		const notifyRepo = await seedRepository(
			page,
			NOTIFY_REPO,
			{},
			{ branches: { main: { files: { "compose.yaml": composeFile(1, UNPULLABLE_IMAGE) } } } },
		);
		const created = await api(page, "POST", "/applications", {
			name: NOTIFY_APP_NAME,
			repositoryId: notifyRepo.id,
			agentId: agent.id,
			branch: "main",
			path: "compose.yaml",
		});
		expect(created.status).toBe(201);
		failingAppId = created.body.id as string;
		await waitForApplication(
			page,
			failingAppId,
			(app) => app.syncStatus === "out_of_sync" && !!app.lastSyncError,
		);

		expect(await waitForMessages(FAILURE_HOOK, 1)).toEqual([
			`Error: deployment failed for ${NOTIFY_APP_NAME}`,
		]);
		// The scoped notification only covers the first application
		await expectNoMessages(SUCCESS_HOOK, SCOPED_HOOK, DISABLED_HOOK);
	});

	test("editing a notification changes what it receives", async ({ page }) => {
		// Dropping the event stops the delivery, enabling the disabled notification starts it
		await clearRequests(SUCCESS_HOOK);
		await clearRequests(DISABLED_HOOK);
		const update = (id: string, body: Record<string, unknown>) =>
			api(page, "PUT", `/notifications/${id}`, {
				enabled: true,
				allApplications: true,
				allAgents: false,
				allRepositories: false,
				events: DEPLOYMENT_EVENTS,
				...body,
			});

		expect(
			(await update(notifications[SUCCESS_HOOK].id, { events: ["agent.offline"] })).status,
		).toBe(200);
		expect((await update(notifications[DISABLED_HOOK].id, {})).status).toBe(200);

		await deployManually(page);

		const message = `Success: deployment succeeded for ${APP_NAME}`;
		expect(await waitForMessages(DISABLED_HOOK, 1)).toEqual([message]);
		await expectNoMessages(SUCCESS_HOOK);
	});

	test("a failing receiver marks the notification as failed until it recovers", async ({
		page,
	}) => {
		await setStatus(DISABLED_HOOK, 500);
		try {
			await deployManually(page);
			await expect
				.poll(async () => (await getNotification(page, "E2E Deploy Disabled"))!.status, {
					timeout: 30_000,
				})
				.toBe("error");

			await gotoApp(page, "/notifications");
			await expect(
				page.locator("[data-slot='card']", { hasText: "E2E Deploy Disabled" }).getByText("Error", {
					exact: true,
				}),
			).toBeVisible();
		} finally {
			await setStatus(DISABLED_HOOK, 200);
		}

		await deployManually(page);
		await expect
			.poll(async () => (await getNotification(page, "E2E Deploy Disabled"))!.status, {
				timeout: 30_000,
			})
			.toBe("success");
	});
});

test.describe("Removal", () => {
	test("delete the application in the UI", async ({ page }) => {
		await gotoApp(page, `/applications/${appId}/settings/general`);
		await page.getByRole("button", { name: "Delete", exact: true }).click();
		await page.getByRole("dialog").getByRole("button", { name: "Proceed" }).click();

		// The hub answers after the agent removed the containers
		await expect(page.getByText(`Application ${APP_NAME} deleted successfully`)).toBeVisible({
			timeout: 30_000,
		});
		await expect(page).toHaveURL("/applications");
		await expect(applicationListEntry(page, APP_NAME)).toHaveCount(0);

		expect((await listApplications(page)).find((item) => item.id === appId)).toBeUndefined();
		expect((await api(page, "GET", `/applications/${appId}`)).status).toBe(404);
		expect(appContainers(appId)).toHaveLength(0);
		expect(deploymentDirExists(project)).toBe(false);
	});

	test("deleting the repository is blocked while the agent is offline", async ({ page }) => {
		const { repo: offlineRepo, appId: id } = await deployInSeparateRepository(
			page,
			OFFLINE_REPO,
			OFFLINE_APP_NAME,
		);
		offlineAppId = id;

		stopAgent();
		await expect
			.poll(async () => (await getAgent(page)).status, { timeout: 30_000 })
			.toBe("offline");
		try {
			await deleteRepositoryInUi(page, OFFLINE_REPO);
			await expect(page.getByText(/agent is offline/)).toBeVisible();

			expect((await api(page, "GET", `/repositories/${offlineRepo.id}`)).status).toBe(200);
			expect((await api(page, "GET", `/applications/${offlineAppId}`)).status).toBe(200);
			expect(appContainers(offlineAppId)).toHaveLength(1);
		} finally {
			startAgent({ hubUrl: HUB_WS_URL, authToken: agent.authToken });
		}
		await expect
			.poll(async () => (await getAgent(page)).status, { timeout: 60_000 })
			.toBe("online");

		// Once the agent is back, the same delete goes through and cleans up
		await deleteRepositoryInUi(page, OFFLINE_REPO);
		await expect(page.getByText(`Repository ${OFFLINE_REPO} deleted successfully`)).toBeVisible({
			timeout: 30_000,
		});
		expect((await api(page, "GET", `/applications/${offlineAppId}`)).status).toBe(404);
		expect(appContainers(offlineAppId)).toHaveLength(0);
	});

	test("deleting the repository deletes its applications", async ({ page }) => {
		const { appId: id } = await deployInSeparateRepository(page, CASCADE_REPO, CASCADE_APP_NAME);
		cascadeAppId = id;
		const cascadeProject = composeProject(cascadeAppId)!;
		expect(deploymentDirExists(cascadeProject)).toBe(true);

		await gotoApp(page, "/applications");
		await expect(applicationListEntry(page, CASCADE_APP_NAME)).toBeVisible();
		await deleteRepositoryInUi(page, CASCADE_REPO);
		// The hub answers after the agent removed the containers
		await expect(page.getByText(`Repository ${CASCADE_REPO} deleted successfully`)).toBeVisible({
			timeout: 30_000,
		});

		expect((await api(page, "GET", `/applications/${cascadeAppId}`)).status).toBe(404);
		await gotoApp(page, "/applications");
		await expect(applicationListEntry(page, CASCADE_APP_NAME)).toHaveCount(0);

		expect(appContainers(cascadeAppId)).toHaveLength(0);
		expect(deploymentDirExists(cascadeProject)).toBe(false);
	});
});
