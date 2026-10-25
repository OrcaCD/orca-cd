import { createHmac } from "node:crypto";
import {
	test,
	expect,
	request as pwRequest,
	type APIRequestContext,
	type Page,
} from "@playwright/test";
import { APP_URL, REGISTRY_HOST } from "./const.ts";
import { agentLogs, stopAgent } from "./helpers/agent-container.ts";
import { api, listRepositories, seedRepository, type ApiRepository } from "./helpers/api.ts";
import {
	appContainers,
	BUSYBOX_IMAGE,
	inspectContainer,
	pullImage,
	removeApplicationContainers,
} from "./helpers/docker.ts";
import { commit } from "./helpers/gitea-mock.ts";
import { gotoApp } from "./helpers/navigation.ts";
import {
	allowAnonymousRegistry,
	containerImageId,
	createLocalImage,
	imageExists,
	imageRef,
	pushImage,
	removeLocalImage,
	requireRegistryAuth,
	startRegistry,
	stopRegistry,
} from "./helpers/registry.ts";
import {
	applicationEvents,
	createOnlineAgent,
	getApplication,
	listApplications,
	newAuthenticatedContext,
	waitForApplication,
	type ApiAgent,
} from "./helpers/setup.ts";

const AGENT_NAME = "E2E Image Agent";
const REPO = "e2e/images";

const WEBHOOK_APP_NAME = "E2E Image Webhook App";
const POLL_APP_NAME = "E2E Image Polling App";
const POLL_IDLE_APP_NAME = "E2E Image Polling Idle App";
const POLL_OFF_APP_NAME = "E2E Image Polling Off App";
const CLEANUP_APP_NAME = "E2E Image Cleanup App";
const LOCAL_ONLY_APP_NAME = "E2E Image Local Only App";
const PRIVATE_APP_NAME = "E2E Image Private Registry App";

const WEBHOOK_PATH = "webhook/compose.yaml";
const POLL_PATH = "poll/compose.yaml";
const POLL_IDLE_PATH = "poll-idle/compose.yaml";
const POLL_OFF_PATH = "poll-off/compose.yaml";
const CLEANUP_PATH = "cleanup/compose.yaml";
const LOCAL_ONLY_PATH = "local-only/compose.yaml";
const PRIVATE_PATH = "private/compose.yaml";

const WEBHOOK_IMAGE = imageRef("e2e/webhook");
const POLL_IMAGE = imageRef("e2e/poll");
const POLL_IDLE_IMAGE = imageRef("e2e/poll-idle");
const POLL_OFF_IMAGE = imageRef("e2e/poll-off");
const CLEANUP_IMAGE_V1 = imageRef("e2e/cleanup", "1");
const CLEANUP_IMAGE_V2 = imageRef("e2e/cleanup", "2");
const LOCAL_ONLY_IMAGE = imageRef("e2e/local-only");
const PRIVATE_IMAGE = imageRef("e2e/private");
const LOCAL_IMAGES = [
	WEBHOOK_IMAGE,
	POLL_IMAGE,
	POLL_IDLE_IMAGE,
	POLL_OFF_IMAGE,
	CLEANUP_IMAGE_V1,
	CLEANUP_IMAGE_V2,
	LOCAL_ONLY_IMAGE,
	PRIVATE_IMAGE,
];

// The agent never polls more often than every 60 seconds; the margin covers the check itself
const POLL_INTERVAL_MS = 60_000;
const POLL_MARGIN_MS = 5_000;

// The hub combines deliveries of a burst within 5 seconds into one pull
const DEBOUNCE_SETTLE_MS = 7_000;

test.describe.configure({ mode: "serial", timeout: 180_000 });

let agent: ApiAgent;
let repo: ApiRepository;
const appIds: Record<string, string> = {};

function composeFile(image: string) {
	return `services:
  app:
    image: ${image}
    command: ["sh", "-c", "while true; do sleep 1; done"]
    # PID 1 ignores SIGTERM, which would make every stop wait for the default 10 seconds
    stop_grace_period: 1s
    healthcheck:
      test: ["CMD", "true"]
      interval: 2s
`;
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

const waitForTick = (enabledAt: number, tick = 1) =>
	sleep(Math.max(0, enabledAt + tick * POLL_INTERVAL_MS + POLL_MARGIN_MS - Date.now()));

test.beforeAll(async ({ browser }) => {
	pullImage(BUSYBOX_IMAGE);
	await startRegistry();
	pushImage(WEBHOOK_IMAGE, "1");
	pushImage(POLL_IMAGE, "1");
	pushImage(POLL_IDLE_IMAGE, "1");
	pushImage(POLL_OFF_IMAGE, "1");
	pushImage(CLEANUP_IMAGE_V1, "1");
	pushImage(CLEANUP_IMAGE_V2, "2");
	pushImage(PRIVATE_IMAGE, "1");
	createLocalImage(LOCAL_ONLY_IMAGE);

	const context = await newAuthenticatedContext(browser);
	try {
		const page = await context.newPage();
		repo = await seedRepository(
			page,
			REPO,
			{},
			{
				branches: {
					main: {
						files: {
							[WEBHOOK_PATH]: composeFile(WEBHOOK_IMAGE),
							[POLL_PATH]: composeFile(POLL_IMAGE),
							[POLL_IDLE_PATH]: composeFile(POLL_IDLE_IMAGE),
							[POLL_OFF_PATH]: composeFile(POLL_OFF_IMAGE),
							[CLEANUP_PATH]: composeFile(CLEANUP_IMAGE_V1),
							[LOCAL_ONLY_PATH]: composeFile(LOCAL_ONLY_IMAGE),
							[PRIVATE_PATH]: composeFile(PRIVATE_IMAGE),
						},
					},
				},
			},
		);
		agent = await createOnlineAgent(page, AGENT_NAME);
	} finally {
		await context.close();
	}
});

test.afterAll(async ({ browser }) => {
	const context = await newAuthenticatedContext(browser);
	try {
		const page = await context.newPage();
		for (const application of await listApplications(page)) {
			await api(page, "DELETE", `/applications/${application.id}`);
		}
		for (const { id, name } of await listRepositories(page)) {
			if (name === REPO) {
				await api(page, "DELETE", `/repositories/${id}`);
			}
		}
		await api(page, "DELETE", `/agents/${agent.id}`);
	} catch {
		// The containers are removed below either way
	} finally {
		await context.close();
		stopAgent();
		Object.values(appIds).forEach(removeApplicationContainers);
		stopRegistry();
		LOCAL_IMAGES.forEach(removeLocalImage);
	}
});

async function deployApplication(
	page: Page,
	key: string,
	name: string,
	path: string,
	extra: Record<string, unknown> = {},
) {
	const created = await api(page, "POST", "/applications", {
		name,
		repositoryId: repo.id,
		agentId: agent.id,
		branch: "main",
		path,
		...extra,
	});
	expect(created.status).toBe(201);
	const id = created.body.id as string;
	appIds[key] = id;
	await waitForApplication(
		page,
		id,
		(app) => app.syncStatus === "synced" && app.healthStatus === "healthy",
	);
	expect(appContainers(id)).toHaveLength(1);
	return id;
}

function runningVersion(id: string) {
	const containers = appContainers(id);
	if (containers.length !== 1) {
		return undefined;
	}
	return inspectContainer(containers[0].id).env.IMAGE_VERSION;
}

async function imageEvents(page: Page, id: string) {
	return (await applicationEvents(page, id)).filter((event) => event.type === "image_update");
}

/** Waits for exactly one new image update event and for it to settle with the given status. */
async function expectNewPull(
	page: Page,
	id: string,
	before: number,
	{ source, status }: { source: string; status: string },
) {
	await expect
		.poll(
			async () => {
				const events = await imageEvents(page, id);
				return events.length === before + 1 ? `${events[0].source}:${events[0].status}` : "";
			},
			{ timeout: 60_000 },
		)
		.toBe(`${source}:${status}`);
	await waitForApplication(page, id, (app) => app.syncStatus === "synced");
}

async function updateApplication(page: Page, id: string, patch: Record<string, unknown>) {
	const current = await getApplication(page, id);
	return api(page, "PUT", `/applications/${id}`, {
		name: current.name,
		repositoryId: current.repositoryId,
		agentId: current.agentId,
		branch: current.branch,
		path: current.path,
		imagePollEnabled: current.imagePollEnabled,
		imagePollIntervalSeconds: current.imagePollIntervalSeconds,
		deleteOldImages: current.deleteOldImages,
		...patch,
	});
}

async function pullImagesFromUi(page: Page, id: string) {
	await gotoApp(page, `/applications/${id}/details`);
	await page.getByRole("button", { name: "More deploy options" }).click();
	await page.getByRole("menuitem", { name: /Pull images/ }).click();
	await expect(page.getByText("Image pull started")).toBeVisible();
}

async function openImageUpdates(page: Page, id: string) {
	await gotoApp(page, `/applications/${id}/settings/image-polling`);
	await expect(page.getByText("Image Pull Webhook")).toBeVisible();
}

test.describe("Image pull webhook", () => {
	let id = "";
	let secret = "";
	let webhook: APIRequestContext;

	test.beforeAll(async () => {
		webhook = await pwRequest.newContext({ baseURL: APP_URL });
	});

	test.afterAll(async () => {
		await webhook.dispose();
	});

	const webhookUrl = () => `${APP_URL}/api/v1/webhooks/images/${id}`;

	/** Sends one delivery, waiting out the hub's per-IP rate limit. */
	async function deliver(
		appId: string,
		{
			headers = {},
			query = "",
			body = "",
		}: { headers?: Record<string, string>; query?: string; body?: string },
	) {
		for (let attempt = 0; attempt < 15; attempt++) {
			const response = await webhook.post(`/api/v1/webhooks/images/${appId}${query}`, {
				headers: { "Content-Type": "application/json", ...headers },
				data: body,
			});
			if (response.status() !== 429) {
				return response.status();
			}
			await sleep(1_000);
		}
		throw new Error("the webhook stayed rate limited");
	}

	const bearer = (token = secret) => ({ Authorization: `Bearer ${token}` });

	function github(event: string, payload: object | string, signingSecret = secret) {
		const body = typeof payload === "string" ? payload : JSON.stringify(payload);
		const signature = createHmac("sha256", signingSecret).update(body).digest("hex");
		return {
			headers: { "X-GitHub-Event": event, "X-Hub-Signature-256": `sha256=${signature}` },
			body,
		};
	}

	const packageEvent = ({
		tag,
		action = "published",
		packageType = "CONTAINER",
	}: {
		tag?: string;
		action?: string;
		packageType?: string;
	}) => ({
		action,
		package: {
			package_type: packageType,
			package_version: { container_metadata: { tag: { name: tag } } },
		},
	});

	test("generate the webhook in the UI", async ({ page }) => {
		id = await deployApplication(page, "webhook", WEBHOOK_APP_NAME, WEBHOOK_PATH);
		expect(runningVersion(id)).toBe("1");
		expect((await getApplication(page, id)).imageWebhookEnabled).toBe(false);

		await openImageUpdates(page, id);
		await expect(page.getByRole("button", { name: "Revoke" })).toHaveCount(0);
		await page.getByRole("button", { name: "Generate Webhook" }).click();

		const dialog = page.getByRole("dialog");
		await expect(dialog.getByText("Webhook Secret")).toBeVisible();
		secret = await dialog.locator(`#app-pull-webhook-secret-${id}`).inputValue();
		expect(secret).not.toBe("");
		await dialog.getByRole("button", { name: "Done" }).click();

		await expect(page.locator("code", { hasText: webhookUrl() })).toBeVisible();
		await expect(page.getByRole("button", { name: "Regenerate" })).toBeVisible();
		await expect(page.getByRole("button", { name: "Revoke" })).toBeVisible();
		expect(await getApplication(page, id)).toMatchObject({
			imageWebhookEnabled: true,
			imageWebhookUrl: webhookUrl(),
		});
	});

	test("rejects deliveries without a valid secret", async ({ page }) => {
		const before = (await imageEvents(page, id)).length;

		expect(await deliver(id, {})).toBe(401);
		expect(await deliver(id, { headers: bearer("wrong-secret") })).toBe(401);
		expect(await deliver(id, { query: "?token=wrong-secret" })).toBe(401);
		expect(await deliver(id, { headers: bearer(secret.slice(0, -1)) })).toBe(401);

		const payload = packageEvent({ tag: "latest" });
		expect(await deliver(id, github("package", payload, "wrong-secret"))).toBe(401);
		expect(
			await deliver(id, {
				headers: { "X-GitHub-Event": "package" },
				body: JSON.stringify(payload),
			}),
		).toBe(401);
		const tampered = github("package", payload);
		expect(
			await deliver(id, { ...tampered, body: JSON.stringify({ ...payload, action: "x" }) }),
		).toBe(401);

		// An unknown application answers like a wrong secret, so existence isn't revealed
		const unknownId = "00000000-0000-0000-0000-000000000000";
		expect(await deliver(unknownId, { headers: bearer() })).toBe(401);
		expect(await deliver(unknownId, github("package", payload))).toBe(401);

		await sleep(DEBOUNCE_SETTLE_MS);
		expect(await imageEvents(page, id)).toHaveLength(before);
	});

	test("rejects or ignores deliveries that are not image pushes", async ({ page }) => {
		const before = (await imageEvents(page, id)).length;
		const digestTag = `sha256-${"a".repeat(64)}`;

		expect(await deliver(id, github("issues", { action: "opened" }))).toBe(400);
		// Playwright would re-encode an invalid JSON string sent as application/json
		const malformed = github("package", "{not json");
		expect(
			await deliver(id, {
				...malformed,
				headers: { ...malformed.headers, "Content-Type": "text/plain" },
			}),
		).toBe(400);

		expect(await deliver(id, github("ping", { zen: "Keep it logically awesome." }))).toBe(204);
		expect(
			await deliver(id, github("package", packageEvent({ tag: "latest", packageType: "npm" }))),
		).toBe(204);
		expect(
			await deliver(id, github("package", packageEvent({ tag: "latest", action: "deleted" }))),
		).toBe(204);
		// Signatures and attestations of an image are pushed as tags of their own
		expect(await deliver(id, github("package", packageEvent({ tag: `${digestTag}.sig` })))).toBe(
			204,
		);
		expect(await deliver(id, github("package", packageEvent({ tag: digestTag })))).toBe(204);
		expect(await deliver(id, github("package", packageEvent({ tag: "" })))).toBe(204);
		expect(
			await deliver(id, {
				headers: bearer(),
				body: JSON.stringify({ event_type: "deleteImage" }),
			}),
		).toBe(204);

		await sleep(DEBOUNCE_SETTLE_MS);
		expect(await imageEvents(page, id)).toHaveLength(before);
		expect(runningVersion(id)).toBe("1");
	});

	const formats = [
		{
			title: "a generic Bearer token",
			delivery: () => ({ headers: bearer() }),
		},
		{
			title: "a Bearer token with a Harbor payload",
			delivery: () => ({
				headers: bearer(),
				body: JSON.stringify({
					type: "PUSH_ARTIFACT",
					event_type: "pushImage",
					event_data: { repository: { name: "webhook" } },
				}),
			}),
		},
		{
			title: "a Docker Hub payload with the token in the URL",
			delivery: () => ({
				query: `?token=${secret}`,
				body: JSON.stringify({
					push_data: { tag: "latest", pushed_at: 1_700_000_000 },
					repository: { repo_name: "e2e/webhook" },
				}),
			}),
		},
		{
			title: "a signed GitHub package event",
			delivery: () => github("package", packageEvent({ tag: "latest" })),
		},
		{
			title: "a signed GitHub package event for an updated tag",
			delivery: () => github("package", packageEvent({ tag: "latest", action: "updated" })),
		},
	];

	for (const { title, delivery } of formats) {
		test(`accepts ${title}`, async ({ page }) => {
			const before = (await imageEvents(page, id)).length;

			expect(await deliver(id, delivery())).toBe(204);

			// The image is already up to date, so the pull is recorded without a redeploy
			await expectNewPull(page, id, before, { source: "image_webhook", status: "no_change" });
			expect(runningVersion(id)).toBe("1");
			expect(appContainers(id)).toHaveLength(1);
		});
	}

	test("combines a burst of deliveries into one pull", async ({ page }) => {
		const before = (await imageEvents(page, id)).length;

		// One push of a multi-arch image fires a delivery for every manifest
		for (let delivery = 0; delivery < 4; delivery++) {
			expect(await deliver(id, github("package", packageEvent({ tag: "latest" })))).toBe(204);
			await sleep(400);
		}
		expect(await deliver(id, { headers: bearer() })).toBe(204);

		await expectNewPull(page, id, before, { source: "image_webhook", status: "no_change" });
		await sleep(DEBOUNCE_SETTLE_MS);
		expect(await imageEvents(page, id)).toHaveLength(before + 1);
	});

	test("deliveries further apart are pulled separately", async ({ page }) => {
		const before = (await imageEvents(page, id)).length;

		expect(await deliver(id, { headers: bearer() })).toBe(204);
		await expectNewPull(page, id, before, { source: "image_webhook", status: "no_change" });
		expect(await deliver(id, { headers: bearer() })).toBe(204);
		await expect
			.poll(async () => (await imageEvents(page, id)).length, { timeout: 60_000 })
			.toBe(before + 2);
	});

	test("a pushed image recreates the container", async ({ page }) => {
		const [container] = appContainers(id);
		const before = (await imageEvents(page, id)).length;
		pushImage(WEBHOOK_IMAGE, "2");

		expect(await deliver(id, github("package", packageEvent({ tag: "latest" })))).toBe(204);

		await expectNewPull(page, id, before, { source: "image_webhook", status: "succeeded" });
		await expect.poll(() => runningVersion(id), { timeout: 60_000 }).toBe("2");
		const [updated] = appContainers(id);
		expect(updated.id).not.toBe(container.id);
		await waitForApplication(
			page,
			id,
			(app) => app.syncStatus === "synced" && app.healthStatus === "healthy",
		);

		await gotoApp(page, `/applications/${id}/history`);
		// The breadcrumb is a list item too and contains the application name
		const item = page
			.getByRole("listitem")
			.filter({ hasText: "Succeeded" })
			.filter({ hasText: "Image webhook" });
		await expect(item).toHaveCount(1);
	});

	test("regenerating the secret invalidates the old one", async ({ page }) => {
		const oldSecret = secret;
		await openImageUpdates(page, id);
		await page.getByRole("button", { name: "Regenerate" }).click();
		const dialog = page.getByRole("dialog");
		await expect(dialog.getByText("Webhook Secret")).toBeVisible();
		secret = await dialog.locator(`#app-pull-webhook-secret-${id}`).inputValue();
		await dialog.getByRole("button", { name: "Done" }).click();
		expect(secret).not.toBe("");
		expect(secret).not.toBe(oldSecret);

		expect(await deliver(id, { headers: bearer(oldSecret) })).toBe(401);
		expect(await deliver(id, github("package", packageEvent({ tag: "latest" }), oldSecret))).toBe(
			401,
		);

		const before = (await imageEvents(page, id)).length;
		expect(await deliver(id, { headers: bearer() })).toBe(204);
		await expectNewPull(page, id, before, { source: "image_webhook", status: "no_change" });
	});

	test("revoke the webhook in the UI", async ({ page }) => {
		await openImageUpdates(page, id);
		await page.getByRole("button", { name: "Revoke" }).click();
		await page.getByRole("dialog").getByRole("button", { name: "Proceed" }).click();
		await expect(page.getByText("Webhook revoked")).toBeVisible();

		await expect(page.getByRole("button", { name: "Generate Webhook" })).toBeVisible();
		await expect(page.getByRole("button", { name: "Revoke" })).toHaveCount(0);
		expect(await getApplication(page, id)).toMatchObject({ imageWebhookEnabled: false });

		const before = (await imageEvents(page, id)).length;
		// Without a webhook the application answers like a wrong secret
		expect(await deliver(id, { headers: bearer() })).toBe(401);
		expect(await deliver(id, github("package", packageEvent({ tag: "latest" })))).toBe(401);
		await sleep(DEBOUNCE_SETTLE_MS);
		expect(await imageEvents(page, id)).toHaveLength(before);
	});
});

test.describe("Image polling", () => {
	let id = "";

	// The control applications are checked while the main one waits for its tick, so the
	// 60 second intervals overlap instead of adding up
	const idle = { id: "", enabledAt: 0, containerIds: [] as string[] };
	const off = { id: "", enabledAt: 0, containerIds: [] as string[] };

	test("rejects an interval below 60 seconds", async ({ page }) => {
		id = await deployApplication(page, "poll", POLL_APP_NAME, POLL_PATH);
		expect(await getApplication(page, id)).toMatchObject({ imagePollEnabled: false });

		await openImageUpdates(page, id);
		await page.getByRole("switch", { name: "Enable image polling" }).click();
		const interval = page.getByLabel("Polling interval (seconds)");
		await expect(interval).toBeVisible();
		await interval.fill("30");
		const saves: string[] = [];
		page.on("request", (request) => {
			if (request.method() === "PUT") {
				saves.push(request.url());
			}
		});
		await page.getByRole("button", { name: "Save Changes" }).click();

		// The browser blocks the submit, so nothing is sent
		await expect
			.poll(() => interval.evaluate((input: HTMLInputElement) => input.validity.rangeUnderflow))
			.toBe(true);
		await expect(page.getByText("Application updated")).toHaveCount(0);
		expect(saves).toEqual([]);
		expect(await getApplication(page, id)).toMatchObject({ imagePollEnabled: false });

		// The API enforces the minimum as well, so other clients can't bypass the form
		for (const seconds of [59, 0, -60]) {
			const response = await updateApplication(page, id, {
				imagePollEnabled: true,
				imagePollIntervalSeconds: seconds,
			});
			expect(response.status, `${seconds} seconds`).toBe(400);
		}
		expect(await getApplication(page, id)).toMatchObject({ imagePollEnabled: false });
	});

	test("disabling polling in the UI stops the checks", async ({ page }) => {
		off.id = await deployApplication(page, "poll-off", POLL_OFF_APP_NAME, POLL_OFF_PATH, {
			imagePollEnabled: true,
			imagePollIntervalSeconds: 60,
		});
		off.enabledAt = Date.now();
		off.containerIds = appContainers(off.id).map((item) => item.id);
		expect(await getApplication(page, off.id)).toMatchObject({ imagePollEnabled: true });

		await openImageUpdates(page, off.id);
		await page.getByRole("switch", { name: "Enable image polling" }).click();
		await page.getByRole("button", { name: "Save Changes" }).click();
		await expect(page.getByText("Application updated")).toBeVisible();
		expect(await getApplication(page, off.id)).toMatchObject({ imagePollEnabled: false });

		pushImage(POLL_OFF_IMAGE, "2");
	});

	test("deploy an application that polls an unchanged image", async ({ page }) => {
		idle.id = await deployApplication(page, "poll-idle", POLL_IDLE_APP_NAME, POLL_IDLE_PATH, {
			imagePollEnabled: true,
			imagePollIntervalSeconds: 60,
		});
		idle.enabledAt = Date.now();
		idle.containerIds = appContainers(idle.id).map((item) => item.id);
		expect(await getApplication(page, idle.id)).toMatchObject({ imagePollEnabled: true });
	});

	test("enable polling in the UI", async ({ page }) => {
		// Saving the polling settings must not drop an existing webhook
		expect((await api(page, "POST", `/applications/${id}/image-webhook`)).status).toBe(200);

		await openImageUpdates(page, id);
		const toggle = page.getByRole("switch", { name: "Enable image polling" });
		await expect(toggle).not.toBeChecked();
		await toggle.click();
		await page.getByLabel("Polling interval (seconds)").fill("60");
		await page.getByRole("button", { name: "Save Changes" }).click();
		await expect(page.getByText("Application updated")).toBeVisible();

		expect(await getApplication(page, id)).toMatchObject({
			imagePollEnabled: true,
			imagePollIntervalSeconds: 60,
			imageWebhookEnabled: true,
		});

		// Cancel goes back to the saved values, not to the ones from before the save
		await page.getByLabel("Polling interval (seconds)").fill("500");
		await page.getByRole("button", { name: "Cancel" }).click();
		await expect(page.getByRole("switch", { name: "Enable image polling" })).toBeChecked();
		await expect(page.getByLabel("Polling interval (seconds)")).toHaveValue("60");

		await gotoApp(page, `/applications/${id}/details`);
		await expect(page.getByText("Polling every 60 s")).toBeVisible();
		await openImageUpdates(page, id);
		await expect(page.getByRole("switch", { name: "Enable image polling" })).toBeChecked();
		await expect(page.getByLabel("Polling interval (seconds)")).toHaveValue("60");
	});

	test("a new image in the registry is rolled out and the old image stays", async ({ page }) => {
		expect(runningVersion(id)).toBe("1");
		const [container] = appContainers(id);
		const oldImageId = containerImageId(container.id);
		const before = (await imageEvents(page, id)).length;
		pushImage(POLL_IMAGE, "2");

		// The first check runs one interval after polling was enabled
		await expect.poll(() => runningVersion(id), { timeout: 150_000 }).toBe("2");
		const [updated] = appContainers(id);
		expect(updated.id).not.toBe(container.id);
		expect(containerImageId(updated.id)).not.toBe(oldImageId);
		await waitForApplication(
			page,
			id,
			(app) => app.syncStatus === "synced" && app.healthStatus === "healthy",
		);

		await expect
			.poll(async () => (await imageEvents(page, id)).length, { timeout: 30_000 })
			.toBe(before + 1);
		const [event] = await imageEvents(page, id);
		expect(event).toMatchObject({ source: "image_polling", status: "succeeded" });

		// Cleanup is opt-in
		expect(imageExists(oldImageId)).toBe(true);

		await gotoApp(page, `/applications/${id}/history`);
		await expect(
			page
				.getByRole("listitem")
				.filter({ hasText: "Succeeded" })
				.filter({ hasText: "Image polling" }),
		).toHaveCount(1);
	});

	test("an unchanged image leaves no history entry", async ({ page }) => {
		await waitForTick(idle.enabledAt);

		expect(await imageEvents(page, idle.id)).toHaveLength(0);
		expect(appContainers(idle.id).map((item) => item.id)).toEqual(idle.containerIds);
		expect(runningVersion(idle.id)).toBe("1");
	});

	test("a new image is ignored once polling is disabled", async ({ page }) => {
		await waitForTick(off.enabledAt);

		expect(await imageEvents(page, off.id)).toHaveLength(0);
		expect(appContainers(off.id).map((item) => item.id)).toEqual(off.containerIds);
		expect(runningVersion(off.id)).toBe("1");
	});
});

test.describe("Delete old images", () => {
	let id = "";
	let previousImageId = "";

	const syncRepository = async (page: Page) => {
		const response = await api(page, "POST", `/repositories/${repo.id}/sync`);
		expect(response.status).toBeLessThan(300);
	};

	test("enable the option in the UI", async ({ page }) => {
		id = await deployApplication(page, "cleanup", CLEANUP_APP_NAME, CLEANUP_PATH);
		expect(runningVersion(id)).toBe("1");
		expect(await getApplication(page, id)).toMatchObject({ deleteOldImages: false });

		await gotoApp(page, `/applications/${id}/settings/general`);
		await expect(page.locator("#name")).toHaveValue(CLEANUP_APP_NAME);
		const toggle = page.getByRole("switch", { name: /Delete old images/ });
		await expect(toggle).not.toBeChecked();
		await toggle.click();
		await expect(toggle).toBeChecked();
		await page.getByRole("button", { name: "Save Changes" }).click();
		await expect(page.getByText("Application updated")).toBeVisible();

		expect(await getApplication(page, id)).toMatchObject({ deleteOldImages: true });
		await page.reload();
		await expect(page.locator("#name")).toHaveValue(CLEANUP_APP_NAME);
		await expect(page.getByRole("switch", { name: /Delete old images/ })).toBeChecked();
	});

	test("a pulled image replaces the old one", async ({ page }) => {
		const [container] = appContainers(id);
		previousImageId = containerImageId(container.id);
		expect(imageExists(previousImageId)).toBe(true);
		const before = (await imageEvents(page, id)).length;
		// Same tag, new content
		pushImage(CLEANUP_IMAGE_V1, "1b");

		await pullImagesFromUi(page, id);

		await expectNewPull(page, id, before, { source: "manual", status: "succeeded" });
		await expect.poll(() => runningVersion(id), { timeout: 60_000 }).toBe("1b");
		const [updated] = appContainers(id);
		const currentImageId = containerImageId(updated.id);
		expect(currentImageId).not.toBe(previousImageId);
		await expect.poll(() => imageExists(previousImageId), { timeout: 30_000 }).toBe(false);
		expect(imageExists(currentImageId)).toBe(true);
		previousImageId = currentImageId;
	});

	test("a redeploy to another image removes the previous one", async ({ page }) => {
		const { sha } = await commit(REPO, {
			files: { [CLEANUP_PATH]: composeFile(CLEANUP_IMAGE_V2) },
			message: "Use the second image",
		});

		await syncRepository(page);

		await waitForApplication(page, id, (app) => app.commit === sha && app.syncStatus === "synced");
		await expect.poll(() => runningVersion(id), { timeout: 60_000 }).toBe("2");
		const [updated] = appContainers(id);
		const currentImageId = containerImageId(updated.id);
		expect(currentImageId).not.toBe(previousImageId);
		await expect.poll(() => imageExists(previousImageId), { timeout: 30_000 }).toBe(false);
		expect(imageExists(currentImageId)).toBe(true);
	});

	test("a pull without a new image keeps the current one", async ({ page }) => {
		const [container] = appContainers(id);
		const currentImageId = containerImageId(container.id);
		const before = (await imageEvents(page, id)).length;

		await pullImagesFromUi(page, id);

		await expectNewPull(page, id, before, { source: "manual", status: "no_change" });
		expect(appContainers(id).map((item) => item.id)).toEqual([container.id]);
		expect(imageExists(currentImageId)).toBe(true);
	});
});

test.describe("Local-only images", () => {
	test("a pull skips an image that was never pushed to a registry", async ({ page }) => {
		const id = await deployApplication(page, "local-only", LOCAL_ONLY_APP_NAME, LOCAL_ONLY_PATH);
		const [container] = appContainers(id);
		const before = (await imageEvents(page, id)).length;

		await pullImagesFromUi(page, id);

		await expectNewPull(page, id, before, { source: "manual", status: "no_change" });
		const [event] = await imageEvents(page, id);
		expect(event.errorMessage).toBeNull();
		expect(appContainers(id).map((item) => item.id)).toEqual([container.id]);
	});
});

test.describe("Private registry", () => {
	const app = { id: "", enabledAt: 0 };

	const failedChecks = () =>
		agentLogs()
			.split("\n")
			.filter((line) => line.includes("failed to check/pull images") && line.includes(app.id))
			.length;

	test.afterAll(async () => {
		await allowAnonymousRegistry();
	});

	test("deploy an application that polls a registry which then requires a login", async ({
		page,
	}) => {
		app.id = await deployApplication(page, "private", PRIVATE_APP_NAME, PRIVATE_PATH, {
			imagePollEnabled: true,
			imagePollIntervalSeconds: 60,
		});
		app.enabledAt = Date.now();

		await requireRegistryAuth();
		expect(runningVersion(app.id)).toBe("1");
	});

	test("the periodic check reports that the registry can't be checked", async ({ page }) => {
		await expect
			.poll(async () => (await imageEvents(page, app.id)).length, { timeout: 120_000 })
			.toBe(1);

		const [event] = await imageEvents(page, app.id);
		expect(event).toMatchObject({ source: "image_polling", status: "failed" });
		expect(event.errorMessage).toContain(`image ${PRIVATE_IMAGE}`);
		expect(event.errorMessage).toContain(`no credentials for ${REGISTRY_HOST} found`);
		await waitForApplication(page, app.id, (current) => current.syncStatus === "out_of_sync");
		expect(runningVersion(app.id)).toBe("1");
	});

	test("a manual pull reports the same failure again", async ({ page }) => {
		const [previous] = await imageEvents(page, app.id);

		await pullImagesFromUi(page, app.id);

		await expect
			.poll(async () => (await imageEvents(page, app.id)).length, { timeout: 60_000 })
			.toBe(2);
		const [event] = await imageEvents(page, app.id);
		expect(event).toMatchObject({ source: "manual", status: "failed" });
		expect(event.errorMessage).toBe(previous.errorMessage);
	});

	test("repeated periodic failures are not recorded again", async ({ page }) => {
		const checks = failedChecks();

		await waitForTick(app.enabledAt, 2);

		expect(failedChecks()).toBeGreaterThan(checks);
		expect(await imageEvents(page, app.id)).toHaveLength(2);
		expect((await getApplication(page, app.id)).syncStatus).toBe("out_of_sync");
	});

	test("the application is in sync again once the registry is reachable", async ({ page }) => {
		await allowAnonymousRegistry();

		await waitForApplication(page, app.id, (current) => current.syncStatus === "synced", {
			timeout: 90_000,
		});
		expect(await imageEvents(page, app.id)).toHaveLength(2);
		expect(runningVersion(app.id)).toBe("1");
	});
});
