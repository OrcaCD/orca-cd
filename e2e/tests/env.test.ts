import { test, expect, type Page } from "@playwright/test";
import { HUB_WS_URL } from "./const.ts";
import { startAgent, stopAgent } from "./helpers/agent-container.ts";
import { api, listRepositories, seedRepository } from "./helpers/api.ts";
import {
	appContainers,
	BUSYBOX_IMAGE,
	composeProject,
	inspectContainer,
	pullImage,
	removeApplicationContainers,
	runWithAgentVolumes,
} from "./helpers/docker.ts";
import { gotoApp } from "./helpers/navigation.ts";
import {
	createOnlineAgent,
	listApplications,
	newAuthenticatedContext,
	waitForApplication,
	type ApiAgent,
} from "./helpers/setup.ts";

const AGENT_NAME = "E2E Env Agent";
const REPO = "e2e/env";
const APP_NAME = "E2E Env App";
const PLAIN_APP_NAME = "E2E Env Plain App";
const PROJECT = "e2e-env-app";
const OUTSIDE_FILE = "/deployments/e2e-env-outside.txt";
const OUTSIDE_CONTENT = "outside-secret-e2e";
const MARKER = "# orcacd:public";

const SERVICE = `    image: ${BUSYBOX_IMAGE}
    pull_policy: if_not_present
    command: ["sh", "-c", "while true; do sleep 1; done"]
    # PID 1 ignores SIGTERM, which would make every stop wait for the default 10 seconds
    stop_grace_period: 1s`;

const ENV_COMPOSE = `services:
  web:
${SERVICE}
    env_file:
      - .env
      - path: optional.env
        required: false
  api:
${SERVICE}
    env_file: config/api.env
  static:
${SERVICE}
`;

const PLAIN_COMPOSE = `services:
  app:
${SERVICE}
`;

test.describe.configure({ mode: "serial", timeout: 120_000 });

interface ApiEnvEntry {
	key: string;
	isSecret: boolean;
	value?: string;
	isSet: boolean;
}

interface ApiEnvFile {
	path: string;
	services: string[];
	revision: string;
	entries: ApiEnvEntry[];
}

interface EnvChangeInput {
	key: string;
	delete?: boolean;
	value?: string;
	isSecret?: boolean;
}

let agent: ApiAgent;
let appId = "";
let plainAppId = "";
// Every update the hub confirmed, to compare with the audit log
let savedUpdates = 0;

test.beforeAll(async ({ browser }) => {
	pullImage(BUSYBOX_IMAGE);

	const context = await newAuthenticatedContext(browser);
	try {
		const page = await context.newPage();
		await seedRepository(
			page,
			REPO,
			{},
			{
				branches: {
					main: {
						files: { "env/compose.yaml": ENV_COMPOSE, "plain/compose.yaml": PLAIN_COMPOSE },
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
		sh(`rm -f ${OUTSIDE_FILE}`);
		stopAgent();
		removeApplicationContainers(appId);
		removeApplicationContainers(plainAppId);
	}
});

function sh(script: string, ...args: string[]) {
	return runWithAgentVolumes(["sh", "-ec", script, "sh", ...args]);
}

function deploymentFile(path: string) {
	const result = runWithAgentVolumes(["cat", `/deployments/${PROJECT}/${path}`]);
	return result.status === 0 ? result.stdout : undefined;
}

function appendToDeploymentFile(path: string, content: string) {
	const result = sh('printf "%s" "$1" >> "$2"', content, `/deployments/${PROJECT}/${path}`);
	expect(result.status, result.stderr).toBe(0);
}

function serviceContainers() {
	return Object.fromEntries(
		appContainers(appId).map((container) => [
			inspectContainer(container.id).labels["com.docker.compose.service"],
			container.id,
		]),
	);
}

function serviceEnv(service: string) {
	return inspectContainer(serviceContainers()[service]).env;
}

function lineOf(content: string | undefined, key: string) {
	const lines = (content ?? "").split("\n");
	const index = lines.findIndex((line) => line.startsWith(`${key}=`));
	return { index, line: lines[index], previous: lines[index - 1] };
}

function listEnv(page: Page, id = appId) {
	return api(page, "GET", `/applications/${id}/env`);
}

async function envFile(page: Page, path: string) {
	const { status, body } = await listEnv(page);
	expect(status).toBe(200);
	const file = (body.files as ApiEnvFile[]).find((item) => item.path === path);
	expect(file, `${path} is listed`).toBeDefined();
	return file!;
}

async function entry(page: Page, path: string, key: string) {
	return (await envFile(page, path)).entries.find((item) => item.key === key);
}

async function putEnv(page: Page, files: unknown) {
	const response = await api(page, "PUT", `/applications/${appId}/env`, { files });
	if (response.status === 200) {
		savedUpdates++;
	}
	return response;
}

async function updateEnv(page: Page, path: string, changes: EnvChangeInput[], revision?: string) {
	return putEnv(page, [
		{
			path,
			expectedRevision: revision ?? (await envFile(page, path)).revision,
			changes: changes.map(({ key, delete: remove = false, value = "", isSecret = false }) => ({
				key,
				delete: remove,
				value,
				isSecret,
			})),
		},
	]);
}

function escapeRegExp(value: string) {
	return value.replace(/[.*+?^${}()|[\]\\/]/g, "\\$&");
}

function envCard(page: Page, path: string) {
	return page.locator("[data-slot='card']").filter({
		has: page.locator("[data-slot='card-title']", {
			hasText: new RegExp(`^${escapeRegExp(path)}$`),
		}),
	});
}

function envRow(page: Page, path: string, key: string) {
	return envCard(page, path).getByRole("row").filter({ hasText: key });
}

async function openEnvironment(page: Page) {
	await gotoApp(page, `/applications/${appId}/settings/environment`);
	await expect(page.getByRole("button", { name: "Add variable" }).first()).toBeVisible();
}

async function addEntry(
	page: Page,
	path: string,
	key: string,
	value: string,
	{ secret = false }: { secret?: boolean } = {},
) {
	await envCard(page, path).getByRole("button", { name: "Add variable" }).click();
	const dialog = page.getByRole("dialog");
	await dialog.locator("#env-add-key").fill(key);
	await dialog.locator("#env-add-value").fill(value);
	if (secret) {
		await dialog.getByRole("switch", { name: "Store as secret" }).click();
	}
	await dialog.getByRole("button", { name: "Add", exact: true }).click();
	await expect(dialog).toBeHidden();
}

async function saveInUi(page: Page, expectedStatus = 200) {
	const [response] = await Promise.all([
		page.waitForResponse(
			(item) =>
				item.request().method() === "PUT" && item.url().endsWith(`/applications/${appId}/env`),
			{ timeout: 90_000 },
		),
		page.getByRole("button", { name: "Save & redeploy" }).click(),
	]);
	expect(response.status()).toBe(expectedStatus);
	if (response.status() === 200) {
		savedUpdates++;
		await expect(page.getByText("Environment saved and application redeployed")).toBeVisible();
		await expect(page.getByRole("button", { name: "Save & redeploy" })).toBeDisabled();
	}
}

async function expectNoSecretInPage(page: Page, ...secrets: string[]) {
	const content = await page.content();
	for (const secret of secrets) {
		expect(content).not.toContain(secret);
	}
}

function expectOnlyRecreated(before: Record<string, string>, ...recreated: string[]) {
	const after = serviceContainers();
	expect(Object.keys(after).toSorted()).toEqual(Object.keys(before).toSorted());
	for (const service of Object.keys(before)) {
		if (recreated.includes(service)) {
			expect(after[service], `${service} is recreated`).not.toBe(before[service]);
		} else {
			expect(after[service], `${service} is untouched`).toBe(before[service]);
		}
	}
}

async function createApplication(page: Page, name: string, path: string) {
	const repository = (await listRepositories(page)).find((item) => item.name === REPO)!;
	const created = await api(page, "POST", "/applications", {
		name,
		repositoryId: repository.id,
		agentId: agent.id,
		branch: "main",
		path,
	});
	expect(created.status).toBe(201);
	const id = created.body.id as string;
	await waitForApplication(page, id, (app) => app.syncStatus === "synced");
	return id;
}

test.describe("Deployment", () => {
	test("creates the missing required env files and skips optional ones", async ({ page }) => {
		appId = await createApplication(page, APP_NAME, "env/compose.yaml");
		await expect.poll(() => appContainers(appId).length, { timeout: 60_000 }).toBe(3);
		expect(composeProject(appId)).toBe(PROJECT);

		expect(deploymentFile(".env")).toBe("");
		expect(deploymentFile("config/api.env")).toBe("");
		expect(deploymentFile("optional.env")).toBeUndefined();
		const mode = sh('stat -c %a "$1"', `/deployments/${PROJECT}/.env`);
		expect(mode.stdout.trim()).toBe("600");
	});

	test("shows an explanation when no env_file is declared", async ({ page }) => {
		plainAppId = await createApplication(page, PLAIN_APP_NAME, "plain/compose.yaml");

		const { status, body } = await listEnv(page, plainAppId);
		expect(status).toBe(200);
		expect(body.files).toEqual([]);

		await gotoApp(page, `/applications/${plainAppId}/settings/environment`);
		await expect(page.getByText("No env files declared")).toBeVisible();
		await expect(page.getByText(/Add an env_file to every service/)).toBeVisible();
		await expect(page.getByRole("button", { name: "Save & redeploy" })).toHaveCount(0);
	});
});

test.describe("Listing", () => {
	test("lists every declared env file with its services", async ({ page }) => {
		const { status, body } = await listEnv(page);
		expect(status).toBe(200);
		const files = body.files as ApiEnvFile[];
		expect(files.map((file) => file.path).toSorted()).toEqual([
			".env",
			"config/api.env",
			"optional.env",
		]);
		for (const file of files) {
			expect(file.entries).toEqual([]);
			expect(file.revision).toBeTruthy();
		}
		expect(files.find((file) => file.path === ".env")!.services).toEqual(["web"]);
		expect(files.find((file) => file.path === "config/api.env")!.services).toEqual(["api"]);
		expect(files.find((file) => file.path === "optional.env")!.services).toEqual(["web"]);

		await gotoApp(page, `/applications/${appId}/settings/general`);
		await page.getByRole("link", { name: "Environment Variables" }).click();
		await expect(page).toHaveURL(`/applications/${appId}/settings/environment`);

		for (const [path, service] of [
			[".env", "web"],
			["config/api.env", "api"],
			["optional.env", "web"],
		]) {
			const card = envCard(page, path);
			await expect(card).toBeVisible();
			await expect(card.getByText(service, { exact: true })).toBeVisible();
			await expect(card.getByText("This env file has no entries yet.")).toBeVisible();
		}
		await expect(page.getByRole("button", { name: "Save & redeploy" })).toBeDisabled();
	});
});

test.describe("Editing in the UI", () => {
	test("adds a variable and a secret and restarts only the service using the file", async ({
		page,
	}) => {
		const before = serviceContainers();
		await openEnvironment(page);

		await addEntry(page, ".env", "LOG_LEVEL", "debug");
		await addEntry(page, ".env", "DB_PASSWORD", "hunter2-e2e", { secret: true });
		await expect(envRow(page, ".env", "LOG_LEVEL").getByText("New", { exact: true })).toBeVisible();
		await expect(page.getByText("Unsaved changes: 2")).toBeVisible();
		expect(deploymentFile(".env")).toBe("");

		await saveInUi(page);

		const content = deploymentFile(".env");
		expect(lineOf(content, "LOG_LEVEL")).toMatchObject({
			line: "LOG_LEVEL=debug",
			previous: MARKER,
		});
		const secret = lineOf(content, "DB_PASSWORD");
		expect(secret.line).toMatch(/^DB_PASSWORD=["']?hunter2-e2e["']?$/);
		expect(secret.previous).not.toBe(MARKER);

		expectOnlyRecreated(before, "web");
		expect(serviceEnv("web")).toMatchObject({ LOG_LEVEL: "debug", DB_PASSWORD: "hunter2-e2e" });

		await expect(page.getByRole("textbox", { name: "LOG_LEVEL", exact: true })).toHaveValue(
			"debug",
		);
		const row = envRow(page, ".env", "DB_PASSWORD");
		await expect(row.getByText("Secret is set")).toBeAttached();
		await expect(row.getByRole("button", { name: "Replace", exact: true })).toBeVisible();
		await expect(page.getByRole("textbox", { name: "DB_PASSWORD" })).toHaveCount(0);

		await openEnvironment(page);
		await expectNoSecretInPage(page, "hunter2-e2e");
	});

	test("never returns the value of a secret through the API", async ({ page }) => {
		const { status, body } = await listEnv(page);
		expect(status).toBe(200);
		expect(JSON.stringify(body)).not.toContain("hunter2-e2e");

		const variable = await entry(page, ".env", "LOG_LEVEL");
		expect(variable).toEqual({ key: "LOG_LEVEL", isSecret: false, value: "debug", isSet: true });
		const secret = await entry(page, ".env", "DB_PASSWORD");
		expect(secret).toEqual({ key: "DB_PASSWORD", isSecret: true, isSet: true });
		expect(secret).not.toHaveProperty("value");
	});

	test("discarding the changes restores the saved state", async ({ page }) => {
		await openEnvironment(page);
		const textbox = page.getByRole("textbox", { name: "LOG_LEVEL", exact: true });
		await textbox.fill("temporary");
		await expect(envRow(page, ".env", "LOG_LEVEL").getByText("Changed")).toBeVisible();
		await expect(page.getByText("Unsaved changes: 1")).toBeVisible();
		await expect(page.getByRole("button", { name: "Save & redeploy" })).toBeEnabled();

		await page.getByRole("button", { name: "Discard changes" }).click();

		await expect(textbox).toHaveValue("debug");
		await expect(page.getByText(/Unsaved changes/)).toHaveCount(0);
		await expect(page.getByRole("button", { name: "Save & redeploy" })).toBeDisabled();
	});

	test("edits a variable and replaces a secret", async ({ page }) => {
		const before = serviceContainers();
		await openEnvironment(page);

		await page.getByRole("textbox", { name: "LOG_LEVEL", exact: true }).fill("info");
		await envRow(page, ".env", "DB_PASSWORD")
			.getByRole("button", { name: "Replace", exact: true })
			.click();
		const dialog = page.getByRole("dialog");
		await dialog.locator("#env-replace-value").fill("rotated-e2e");
		await dialog.getByRole("button", { name: "Replace", exact: true }).click();
		await expect(dialog).toBeHidden();
		await expect(page.getByText("Unsaved changes: 2")).toBeVisible();

		await saveInUi(page);

		const content = deploymentFile(".env");
		expect(lineOf(content, "LOG_LEVEL")).toMatchObject({
			line: "LOG_LEVEL=info",
			previous: MARKER,
		});
		expect(lineOf(content, "DB_PASSWORD").line).toMatch(/^DB_PASSWORD=["']?rotated-e2e["']?$/);
		expect(content).not.toContain("hunter2-e2e");
		expectOnlyRecreated(before, "web");
		expect(serviceEnv("web")).toMatchObject({ LOG_LEVEL: "info", DB_PASSWORD: "rotated-e2e" });
		await expectNoSecretInPage(page, "rotated-e2e");
	});

	test("deletes a variable", async ({ page }) => {
		const before = serviceContainers();
		await openEnvironment(page);
		const row = envRow(page, ".env", "LOG_LEVEL");

		await row.getByRole("button", { name: "Delete LOG_LEVEL" }).click();
		await expect(row.getByText("Will be deleted")).toBeVisible();
		await row.getByRole("button", { name: "Undo" }).click();
		await expect(row.getByText("Will be deleted")).toHaveCount(0);
		await expect(page.getByRole("button", { name: "Save & redeploy" })).toBeDisabled();

		await row.getByRole("button", { name: "Delete LOG_LEVEL" }).click();
		await saveInUi(page);

		const content = deploymentFile(".env");
		expect(content).not.toContain("LOG_LEVEL");
		expect(lineOf(content, "DB_PASSWORD").line).toContain("rotated-e2e");
		expect(await entry(page, ".env", "LOG_LEVEL")).toBeUndefined();
		expectOnlyRecreated(before, "web");
		expect(serviceEnv("web").LOG_LEVEL).toBeUndefined();
		await expect(envRow(page, ".env", "LOG_LEVEL")).toHaveCount(0);
	});

	test("treats keys of an existing file as secrets and keeps its comments", async ({ page }) => {
		const legacy = "# my comment\nLEGACY_TOKEN=legacy-e2e\nPORT=8080\n";
		appendToDeploymentFile("config/api.env", legacy);

		await openEnvironment(page);
		for (const key of ["LEGACY_TOKEN", "PORT"]) {
			const row = envRow(page, "config/api.env", key);
			await expect(row.getByRole("button", { name: "Replace", exact: true })).toBeVisible();
			await expect(row.getByText("Secret is set")).toBeAttached();
		}
		await expectNoSecretInPage(page, "legacy-e2e");
		const legacyEntry = await entry(page, "config/api.env", "LEGACY_TOKEN");
		expect(legacyEntry).toEqual({ key: "LEGACY_TOKEN", isSecret: true, isSet: true });

		const before = serviceContainers();
		await addEntry(page, "config/api.env", "NEW_VAR", "visible-e2e");
		await saveInUi(page);

		const content = deploymentFile("config/api.env")!;
		expect(content.startsWith(legacy)).toBe(true);
		expect(lineOf(content, "NEW_VAR")).toMatchObject({
			line: "NEW_VAR=visible-e2e",
			previous: MARKER,
		});
		expectOnlyRecreated(before, "api");
		expect(serviceEnv("api")).toMatchObject({
			LEGACY_TOKEN: "legacy-e2e",
			PORT: "8080",
			NEW_VAR: "visible-e2e",
		});
	});

	test("turns a variable into a secret", async ({ page }) => {
		await openEnvironment(page);
		const row = envRow(page, "config/api.env", "NEW_VAR");
		await row.getByRole("button", { name: "Make secret" }).click();
		await expect(row.getByText("Changed")).toBeVisible();
		await saveInUi(page);

		const content = deploymentFile("config/api.env");
		const line = lineOf(content, "NEW_VAR");
		expect(line.line).toContain("visible-e2e");
		expect(line.previous).not.toBe(MARKER);
		const secret = await entry(page, "config/api.env", "NEW_VAR");
		expect(secret).toEqual({ key: "NEW_VAR", isSecret: true, isSet: true });
		await expectNoSecretInPage(page, "visible-e2e");
		await expect(row.getByRole("button", { name: "Replace", exact: true })).toBeVisible();
	});

	test("creates a missing optional env file on the first write", async ({ page }) => {
		const before = serviceContainers();
		await openEnvironment(page);
		await addEntry(page, "optional.env", "OPT_E2E", "present");
		await saveInUi(page);

		expect(lineOf(deploymentFile("optional.env"), "OPT_E2E")).toMatchObject({
			line: "OPT_E2E=present",
			previous: MARKER,
		});
		expectOnlyRecreated(before, "web");
		expect(serviceEnv("web").OPT_E2E).toBe("present");
	});
});

test.describe("Rejected updates", () => {
	test("a secret cannot become a variable and nothing is written", async ({ page }) => {
		const file = await envFile(page, "config/api.env");
		const content = deploymentFile("config/api.env");

		const response = await updateEnv(
			page,
			"config/api.env",
			[{ key: "PORT", value: "9090", isSecret: false }],
			file.revision,
		);

		expect(response.status).toBe(502);
		expect(response.body.error).toMatch(/cannot be made a variable/);
		expect(deploymentFile("config/api.env")).toBe(content);
		expect((await envFile(page, "config/api.env")).revision).toBe(file.revision);
	});

	test("an update across files is all or nothing", async ({ page }) => {
		const web = await envFile(page, ".env");
		const apiFile = await envFile(page, "config/api.env");
		const before = serviceContainers();

		const response = await putEnv(page, [
			{
				path: ".env",
				expectedRevision: web.revision,
				changes: [{ key: "ATOMIC_E2E", delete: false, value: "1", isSecret: false }],
			},
			{
				path: "config/api.env",
				expectedRevision: apiFile.revision,
				changes: [{ key: "PORT", delete: false, value: "1", isSecret: false }],
			},
		]);

		expect(response.status).toBe(502);
		expect(deploymentFile(".env")).not.toContain("ATOMIC_E2E");
		expect((await envFile(page, ".env")).revision).toBe(web.revision);
		expectOnlyRecreated(before);
	});

	test("a changed file rejects a stale revision", async ({ page }) => {
		const stale = (await envFile(page, ".env")).revision;
		appendToDeploymentFile(".env", "CHANGED_ELSEWHERE=1\n");

		const response = await updateEnv(page, ".env", [{ key: "STALE_E2E", value: "1" }], stale);

		expect(response.status).toBe(409);
		expect(response.body.conflict).toBe(true);
		expect(deploymentFile(".env")).not.toContain("STALE_E2E");
		expect((await envFile(page, ".env")).revision).not.toBe(stale);
	});

	const invalidRequests: { title: string; build: (revision: string) => unknown }[] = [
		{ title: "no files", build: () => [] },
		{
			title: "a file without changes",
			build: (revision) => [{ path: ".env", expectedRevision: revision, changes: [] }],
		},
		{
			title: "a missing revision",
			build: () => [
				{
					path: ".env",
					expectedRevision: "",
					changes: [{ key: "A_E2E", delete: false, value: "1", isSecret: false }],
				},
			],
		},
		{
			title: "a key with a space",
			build: (revision) => change(revision, { key: "BAD KEY", value: "1" }),
		},
		{
			title: "a key with an equals sign",
			build: (revision) => change(revision, { key: "A=B", value: "1" }),
		},
		{ title: "an empty key", build: (revision) => change(revision, { key: "", value: "1" }) },
		{
			title: "a value with a control character",
			build: (revision) => change(revision, { key: "A_E2E", value: "leak-e2e-\u0001" }),
		},
		{
			title: "a value over 16 KiB",
			build: (revision) => change(revision, { key: "A_E2E", value: "a".repeat(16 * 1024 + 1) }),
		},
		{
			title: "a key listed twice",
			build: (revision) => [
				{
					path: ".env",
					expectedRevision: revision,
					changes: [
						{ key: "A_E2E", delete: false, value: "1", isSecret: false },
						{ key: "A_E2E", delete: false, value: "2", isSecret: false },
					],
				},
			],
		},
		{
			title: "a file listed twice",
			build: (revision) => [
				...change(revision, { key: "A_E2E", value: "1" }),
				...change(revision, { key: "B_E2E", value: "1" }),
			],
		},
	];

	function change(revision: string, { key, value }: { key: string; value: string }) {
		return [
			{
				path: ".env",
				expectedRevision: revision,
				changes: [{ key, delete: false, value, isSecret: false }],
			},
		];
	}

	for (const { title, build } of invalidRequests) {
		test(`the hub rejects ${title}`, async ({ page }) => {
			const file = await envFile(page, ".env");
			const content = deploymentFile(".env");

			const response = await putEnv(page, build(file.revision));

			expect(response.status).toBe(400);
			expect(response.body.error).toBeTruthy();
			expect(JSON.stringify(response.body)).not.toContain("leak-e2e");
			expect(deploymentFile(".env")).toBe(content);
			expect((await envFile(page, ".env")).revision).toBe(file.revision);
		});
	}

	test("the UI validates keys and values before saving", async ({ page }) => {
		await openEnvironment(page);
		await envCard(page, ".env").getByRole("button", { name: "Add variable" }).click();
		const dialog = page.getByRole("dialog");

		await dialog.getByRole("button", { name: "Add", exact: true }).click();
		await expect(dialog.getByText("Key is required")).toBeVisible();
		await expect(dialog.getByText("Value is required")).toBeVisible();

		await dialog.locator("#env-add-key").fill("BAD KEY");
		await dialog.locator("#env-add-value").fill("1");
		await dialog.getByRole("button", { name: "Add", exact: true }).click();
		await expect(dialog.getByText(/Key may only contain letters/)).toBeVisible();

		await dialog.locator("#env-add-key").fill("DB_PASSWORD");
		await dialog.getByRole("button", { name: "Add", exact: true }).click();
		await expect(dialog.getByText("This key already exists in this env file")).toBeVisible();
		await expect(dialog).toBeVisible();

		await dialog.getByRole("button", { name: "Cancel" }).click();
		await expect(dialog).toBeHidden();
		await expect(page.getByRole("button", { name: "Save & redeploy" })).toBeDisabled();
	});
});

test.describe("Path confinement", () => {
	test("rejects files that are not declared in the compose file", async ({ page }) => {
		const revision = (await envFile(page, ".env")).revision;
		const escape = `/deployments/e2e-env-escape`;

		for (const path of ["other.env", "../e2e-env-escape", "/deployments/e2e-env-escape"]) {
			const response = await updateEnv(page, path, [{ key: "A_E2E", value: "1" }], revision);
			expect(response.status, path).toBe(502);
			expect(response.body.error, path).toMatch(/not declared in the compose file/);
		}

		expect(deploymentFile("other.env")).toBeUndefined();
		expect(sh('test -e "$1"', escape).status).not.toBe(0);
		expect(sh('test -e "$1"', `${escape}.env`).status).not.toBe(0);
	});

	test("does not follow a symlink out of the application directory", async ({ page }) => {
		const dir = `/deployments/${PROJECT}`;
		const setup = sh(
			'printf "%s" "$1" > "$2"; mv "$3/optional.env" "$3/optional.env.bak"; ln -s "$2" "$3/optional.env"',
			OUTSIDE_CONTENT,
			OUTSIDE_FILE,
			dir,
		);
		expect(setup.status, setup.stderr).toBe(0);
		try {
			const listed = await listEnv(page);
			expect(listed.status).toBe(502);
			expect(JSON.stringify(listed.body)).not.toContain(OUTSIDE_CONTENT);

			const response = await updateEnv(
				page,
				"optional.env",
				[{ key: "ESCAPE_E2E", value: "1" }],
				"any-revision",
			);
			expect(response.status).not.toBe(200);
			expect(JSON.stringify(response.body)).not.toContain(OUTSIDE_CONTENT);

			expect(sh('cat "$1"', OUTSIDE_FILE).stdout).toBe(OUTSIDE_CONTENT);
		} finally {
			const restore = sh(
				'rm -f "$1/optional.env" "$2"; mv "$1/optional.env.bak" "$1/optional.env"',
				dir,
				OUTSIDE_FILE,
			);
			expect(restore.status, restore.stderr).toBe(0);
		}
		expect((await listEnv(page)).status).toBe(200);
	});
});

test.describe("Concurrent edits", () => {
	test("a conflict is reported and the local changes are discarded on reload", async ({ page }) => {
		const before = serviceContainers();
		await openEnvironment(page);
		await expect(envRow(page, ".env", "DB_PASSWORD")).toBeVisible();

		// Someone else changes the file after the page loaded it
		appendToDeploymentFile(".env", "ELSEWHERE_E2E=1\n");
		await addEntry(page, ".env", "UI_ONLY_E2E", "local");
		await saveInUi(page, 409);

		const dialog = page.getByRole("dialog");
		await expect(dialog.getByText("The environment changed in the meantime")).toBeVisible();
		await dialog.getByRole("button", { name: "Reload" }).click();
		await expect(dialog).toBeHidden();

		await expect(envRow(page, ".env", "UI_ONLY_E2E")).toHaveCount(0);
		await expect(envRow(page, ".env", "ELSEWHERE_E2E")).toBeVisible();
		await expect(page.getByRole("button", { name: "Save & redeploy" })).toBeDisabled();
		expect(deploymentFile(".env")).not.toContain("UI_ONLY_E2E");
		expectOnlyRecreated(before);
	});
});

test.describe("Warnings", () => {
	test("warns before renaming or deleting an application", async ({ page }) => {
		await gotoApp(page, `/applications/${appId}/settings/general`);
		await expect(page.locator("#name")).toHaveValue(APP_NAME);
		await expect(page.getByText(/Its env files and data on the agent will be lost/)).toHaveCount(0);

		await page.locator("#name").fill(`${APP_NAME} Renamed`);
		await expect(page.getByText(/Its env files and data on the agent will be lost/)).toBeVisible();
		await page.reload();

		await page.getByRole("button", { name: "Delete", exact: true }).click();
		await expect(page.getByRole("dialog")).toContainText(
			"env files, secrets and data on the agent are permanently deleted",
		);
		await page.keyboard.press("Escape");
		await expect(page.getByRole("dialog")).toBeHidden();
		expect(appContainers(appId)).toHaveLength(3);
	});
});

test.describe("Audit log", () => {
	test("records one entry per confirmed update", async ({ page }) => {
		expect(savedUpdates).toBeGreaterThan(0);
		const entries = async () => {
			const { status, body } = await api(
				page,
				"GET",
				"/admin/audit-logs?limit=100&search=updated-environment",
			);
			expect(status).toBe(200);
			return (
				body.items as { eventType: string; targetType: string; targetId: string; user: unknown }[]
			).filter((item) => item.targetId === appId);
		};

		await expect.poll(async () => (await entries()).length).toBe(savedUpdates);
		for (const item of await entries()) {
			expect(item).toMatchObject({ eventType: "updated-environment", targetType: "application" });
			expect(item.user).toBeTruthy();
		}
		// The values never end up in the audit log
		expect(JSON.stringify(await entries())).not.toMatch(/rotated-e2e|legacy-e2e|visible-e2e/);

		await gotoApp(page, "/admin/audit-log");
		await expect(page.getByText("UPDATED-ENVIRONMENT").first()).toBeVisible();
	});
});

test.describe("Agent offline", () => {
	test("reports it instead of waiting and recovers once the agent is back", async ({
		page,
		context,
	}) => {
		const apiPage = await context.newPage();
		const agentStatus = async () =>
			(await api(apiPage, "GET", `/agents/${agent.id}`)).body.status as string;

		stopAgent();
		try {
			await expect.poll(agentStatus, { timeout: 30_000 }).toBe("offline");

			const listed = await listEnv(apiPage);
			expect(listed.status).toBe(503);
			expect(listed.body.error).toMatch(/agent is offline/);

			const file = { path: ".env", expectedRevision: "x" };
			const updated = await putEnv(apiPage, [
				{ ...file, changes: [{ key: "OFFLINE_E2E", delete: false, value: "1", isSecret: false }] },
			]);
			expect(updated.status).toBe(503);

			await openEnvironmentPageOffline(page);
		} finally {
			startAgent({ hubUrl: HUB_WS_URL, authToken: agent.authToken });
		}
		await expect.poll(agentStatus, { timeout: 60_000 }).toBe("online");

		// Live updates may refetch the environment before the button is used
		const reload = page.getByRole("button", { name: "Reload", exact: true });
		await expect(async () => {
			if (await reload.isVisible()) {
				await reload.click();
			}
			await expect(envCard(page, ".env")).toBeVisible({ timeout: 2_000 });
		}).toPass({ timeout: 30_000 });
		await expect(page.getByText("The agent is offline")).toHaveCount(0);
		await expect(envRow(page, ".env", "DB_PASSWORD")).toBeVisible();
	});

	async function openEnvironmentPageOffline(page: Page) {
		await gotoApp(page, `/applications/${appId}/settings/environment`);
		const alert = page.getByRole("alert").filter({ hasText: "The agent is offline" });
		await expect(alert).toBeVisible();
		await expect(alert.getByRole("button", { name: "Reload" })).toBeVisible();
		await expect(page.getByRole("button", { name: "Save & redeploy" })).toHaveCount(0);
	}
});
