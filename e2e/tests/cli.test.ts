import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import {
	test,
	expect,
	type Browser,
	type BrowserContext,
	type Cookie,
	type Page,
} from "@playwright/test";
import { ADMIN_EMAIL, ADMIN_NAME, CLI_HUB_CONTAINER, CLI_URL, USER_ROLE_ID } from "./const.ts";
import { api, type ApiRepository } from "./helpers/api.ts";
import {
	cleanupCliHub,
	dataFileSize,
	execHubCli,
	initCliDataVolume,
	listDataFiles,
	readDataFile,
	runHubCli,
	startCliHub,
	stopCliHub,
	writeDataFile,
} from "./helpers/cli-hub.ts";
import { createRepo, deleteRepo, repoUrl, requests } from "./helpers/gitea-mock.ts";
import {
	OIDC_MOCK_ISSUER,
	registerMockClient,
	setMockUser,
	signInAtMock,
} from "./helpers/oidc-mock.ts";
import {
	clickProvider,
	uniqueId,
	useNewClientIp,
	type ApiAdminUser,
	type ApiOidcProvider,
} from "./helpers/oidc.ts";
import {
	credentials,
	enroll,
	importCredential,
	installAuthenticator,
	type TestCredential,
} from "./helpers/passkeys.ts";
import {
	clearRequests,
	hookUrl,
	receivedRequests,
	setStatus,
	waitForMessages,
} from "./helpers/receiver-mock.ts";
import { type ApiAgent, type ApiApplication } from "./helpers/setup.ts";
import { readSetupCode } from "./helpers/setup-code.ts";
import { signInUi, signOutUi } from "./helpers/users.ts";

const SECRET_1 = "cli_e2e_initial_secret_minimum_32_characters";
const SECRET_2 = "cli_e2e_rotated_secret_minimum_32_characters";
const SECRET_3 = "cli_e2e_third_secret_for_repeat_rotation_32ch";
const WRONG_SECRET = "cli_e2e_wrong_secret_that_does_not_match_32ch";

const AGENT_NAME = "CLI Test Agent";
const REPO_NAME = "e2e/cli-main";
const REPO_TOKEN = "cli-gitea-secret-token";
const APP_NAME = "CLI Test App";
const COMPOSE_FILE = "services:\n  web:\n    image: nginx:alpine\n";
const NOTIFY_HOOK = "cli-notify";
const NOTIFY_NAME = "CLI Test Notification";
const NOTIFY_AUTH_HEADER = "Bearer cli-notify-secret";
const LOCAL_USER_EMAIL = "cli-local@example.com";

const envFor = (secret: string, extra: Record<string, string> = {}) => ({
	APP_URL: CLI_URL,
	APP_SECRET: secret,
	...extra,
});

function cliApi(page: Page, method: string, path: string, body?: unknown) {
	return api(page, method, path, body, CLI_URL);
}

function jwtFromAgentToken(authToken: string) {
	return authToken.split(".").slice(0, 3).join(".");
}

function extractEnrollmentCode(output: string) {
	const match = /Enrollment code:\s+(\S+)/.exec(output);
	expect(match, output).not.toBeNull();
	return match![1];
}

// db.Restore names its rollback copy with Unix seconds, so back-to-back imports in the same second collide.
async function waitForNextSecond() {
	const currentSecond = Math.floor(Date.now() / 1000);
	while (Math.floor(Date.now() / 1000) <= currentSecond) {
		await new Promise((resolve) => setTimeout(resolve, 100));
	}
}

function writeModifiedBackup(sourceRelativePath: string, sql: string, targetRelativePath: string) {
	const dir = mkdtempSync(join(tmpdir(), "orca-cli-backup-"));
	try {
		const localPath = join(dir, "backup.db");
		writeFileSync(localPath, readDataFile(sourceRelativePath));
		const db = new DatabaseSync(localPath);
		try {
			db.exec(sql);
		} finally {
			db.close();
		}
		writeDataFile(targetRelativePath, readFileSync(localPath));
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
}

async function openContext(browser: Browser, cookies?: Cookie[]) {
	const context = await browser.newContext({
		baseURL: CLI_URL,
		storageState: { cookies: [], origins: [] },
		locale: "en-US",
		colorScheme: "light",
	});
	await useNewClientIp(context);
	if (cookies) {
		await context.addCookies(cookies);
	}
	return { context, page: await context.newPage() };
}

async function signInWithPasskey(browser: Browser, credential: TestCredential) {
	const session = await openContext(browser);
	await importCredential(session.page, credential);
	await signInUi(session.page);
	await session.page.waitForURL("/");
	return session;
}

test.describe.configure({ mode: "serial" });

let adminCredential: TestCredential;
let adminCookies: Cookie[];

let localUserId = "";
let localUserCredential: TestCredential;
let localUserCookies: Cookie[];

let oidcProvider: ApiOidcProvider;
let oidcUserSub = "";
let oidcUserEmail = "";

let agent: ApiAgent;
let agentAuthToken = "";
let repository: ApiRepository;
let repoWebhookSecret = "";
let application: ApiApplication;
let imageWebhookSecret = "";
let notificationId = "";

test.beforeAll(async ({ browser }) => {
	test.setTimeout(180_000);

	initCliDataVolume();
	await startCliHub(SECRET_1);

	await clearRequests(NOTIFY_HOOK);
	await setStatus(NOTIFY_HOOK, 200);
	await createRepo(REPO_NAME, {
		token: REPO_TOKEN,
		branches: { main: { files: { "compose.yaml": COMPOSE_FILE } } },
	});

	// First-run setup creates the admin account with a passkey
	const setupCode = await readSetupCode(CLI_HUB_CONTAINER);
	const adminSession = await openContext(browser);
	try {
		await installAuthenticator(adminSession.page);
		await adminSession.page.goto(`/register#setup-code=${setupCode}`);
		await expect(adminSession.page.getByRole("textbox", { name: "name" })).toBeVisible();
		await adminSession.page.getByRole("textbox", { name: "name" }).fill(ADMIN_NAME);
		await adminSession.page.getByRole("textbox", { name: "email" }).fill(ADMIN_EMAIL);
		await adminSession.page.locator("form button[type='submit']").click();
		await adminSession.page.waitForURL("/");

		adminCredential = (await credentials(adminSession.page))[0];
		expect(adminCredential).toBeDefined();
		adminCookies = (await adminSession.context.storageState()).cookies;

		// Seed every model that stores encrypted fields so CLI commands operate on realistic data
		const createdAgent = await cliApi(adminSession.page, "POST", "/agents", { name: AGENT_NAME });
		expect(createdAgent.status, JSON.stringify(createdAgent.body)).toBe(201);
		agent = createdAgent.body as ApiAgent;
		agentAuthToken = agent.authToken ?? "";
		expect(agentAuthToken).not.toBe("");

		const createdRepo = await cliApi(adminSession.page, "POST", "/repositories", {
			url: repoUrl(REPO_NAME),
			provider: "gitea",
			authMethod: "token",
			authToken: REPO_TOKEN,
			syncType: "webhook",
		});
		expect(createdRepo.status, JSON.stringify(createdRepo.body)).toBe(201);
		repository = createdRepo.body as ApiRepository;
		repoWebhookSecret = repository.webhookSecret ?? "";
		expect(repoWebhookSecret).not.toBe("");

		const createdApp = await cliApi(adminSession.page, "POST", "/applications", {
			name: APP_NAME,
			agentId: agent.id,
			repositoryId: repository.id,
			branch: "main",
			path: "compose.yaml",
		});
		expect(createdApp.status, JSON.stringify(createdApp.body)).toBe(201);
		application = createdApp.body as ApiApplication;

		const imageWebhook = await cliApi(
			adminSession.page,
			"POST",
			`/applications/${application.id}/image-webhook`,
		);
		expect(imageWebhook.status, JSON.stringify(imageWebhook.body)).toBe(200);
		imageWebhookSecret = (imageWebhook.body as { secret: string }).secret;
		expect(imageWebhookSecret).not.toBe("");

		const createdNotification = await cliApi(adminSession.page, "POST", "/notifications", {
			name: NOTIFY_NAME,
			type: "webhook",
			enabled: true,
			allApplications: true,
			allAgents: false,
			allRepositories: false,
			events: ["application.deployment.succeeded"],
			config: JSON.stringify({
				webhookUrl: hookUrl(NOTIFY_HOOK),
				headers: { Authorization: NOTIFY_AUTH_HEADER },
			}),
		});
		expect(createdNotification.status, JSON.stringify(createdNotification.body)).toBe(201);
		notificationId = (createdNotification.body as { id: string }).id;

		const clientId = uniqueId("cli-oidc-client");
		const clientSecret = uniqueId("cli-oidc-secret");
		const createdProvider = await cliApi(adminSession.page, "POST", "/admin/oidc-providers", {
			name: uniqueId("CLI SSO"),
			issuerUrl: OIDC_MOCK_ISSUER,
			clientId,
			clientSecret,
			enabled: true,
			requireVerifiedEmail: true,
			autoSignup: true,
		});
		expect(createdProvider.status, JSON.stringify(createdProvider.body)).toBe(201);
		oidcProvider = createdProvider.body as ApiOidcProvider;
		await registerMockClient(clientId, clientSecret, [oidcProvider.callbackUrl]);

		// Create a second local user and enroll their passkey
		const createdUser = await cliApi(adminSession.page, "POST", "/admin/users", {
			name: "CLI Local User",
			email: LOCAL_USER_EMAIL,
			roleIds: [USER_ROLE_ID],
		});
		expect(createdUser.status, JSON.stringify(createdUser.body)).toBe(201);
		localUserId = (createdUser.body as { id: string }).id;
		const localEnrollmentToken = (createdUser.body as { enrollmentToken: string }).enrollmentToken;

		const localSession = await openContext(browser);
		try {
			localUserCredential = await enroll(localSession.page, localEnrollmentToken);
			localUserCookies = (await localSession.context.storageState()).cookies;
		} finally {
			await localSession.context.close();
		}

		// Sign in once via OIDC to create a managed (non-local) user
		oidcUserSub = uniqueId("cli-oidc-user");
		oidcUserEmail = `${oidcUserSub}@example.com`;
		await setMockUser(oidcUserSub, {
			email: oidcUserEmail,
			email_verified: true,
			name: "CLI Managed User",
		});

		const oidcSession = await openContext(browser);
		try {
			await clickProvider(oidcSession.page, oidcProvider);
			await signInAtMock(oidcSession.page, oidcUserSub);
			await oidcSession.page.waitForURL("/");
		} finally {
			await oidcSession.context.close();
		}
	} finally {
		await adminSession.context.close();
	}
});

test.afterAll(async () => {
	cleanupCliHub();
	await deleteRepo(REPO_NAME).catch(() => undefined);
	await clearRequests(NOTIFY_HOOK).catch(() => undefined);
});

test.describe("version, help and openapi", () => {
	test("version, --version and -v print the build version", () => {
		const version = runHubCli(["version"]);
		expect(version.status, version.stderr).toBe(0);
		expect(version.stdout).toMatch(/^\S+ \(commit: \S+, built: \S+\)$/);

		for (const flag of ["--version", "-v"]) {
			const res = runHubCli([flag]);
			expect(res.status, flag).toBe(0);
			expect(res.stdout, flag).toBe(version.stdout);
		}
	});

	test("--help and -h list the available commands", () => {
		for (const flag of ["--help", "-h"]) {
			const res = runHubCli([flag]);
			expect(res.status, flag).toBe(0);
			for (const command of [
				"export",
				"healthcheck",
				"import",
				"key",
				"openapi",
				"reset-passkeys",
				"version",
			]) {
				expect(res.stdout, `${flag} includes ${command}`).toContain(command);
			}
		}

		const keyHelp = runHubCli(["key", "--help"]);
		expect(keyHelp.status, keyHelp.stderr).toBe(0);
		expect(keyHelp.stdout).toContain("rotate");
	});

	test("openapi prints the OpenAPI 3.1 spec without configuration or database", () => {
		const res = runHubCli(["openapi"]);
		expect(res.status, res.stderr).toBe(0);

		const spec = JSON.parse(res.stdout) as {
			openapi: string;
			info: { title: string };
			paths: Record<string, unknown>;
			components: { schemas: Record<string, unknown> };
		};
		expect(spec.openapi).toBe("3.1.0");
		expect(spec.info.title).toBeTruthy();
		for (const path of [
			"/health",
			"/auth/profile",
			"/applications/{id}/deploy",
			"/repositories",
			"/agents",
			"/notifications",
		]) {
			expect(spec.paths, path).toHaveProperty(path);
		}
		expect(Object.keys(spec.components.schemas).length).toBeGreaterThan(0);
	});
});

test.describe("healthcheck", () => {
	test("succeeds against a running hub", () => {
		const res = execHubCli(["healthcheck"]);
		expect(res.status, res.stderr).toBe(0);
		expect(res.stdout).toBe("ok");
	});

	test("fails when the hub is unreachable or misconfigured", () => {
		const wrongPort = execHubCli(["healthcheck"], { env: { PORT: "19999" } });
		expect(wrongPort.status).toBe(1);
		expect(wrongPort.stderr).toContain("health check failed");

		const noServer = runHubCli(["healthcheck"], { env: envFor(SECRET_1) });
		expect(noServer.status).toBe(1);
		expect(noServer.stderr).toContain("health check failed");

		const invalidConfig = runHubCli(["healthcheck"]);
		expect(invalidConfig.status).toBe(1);
		expect(invalidConfig.stderr).toContain("invalid configuration");
	});
});

test.describe("export", () => {
	test("creates a backup while the hub is running", () => {
		const res = execHubCli(["export", "-o", "live-export.db"]);
		expect(res.status, res.stderr).toBe(0);
		expect(res.stdout).toContain("Export Successful");
		expect(res.stdout).toContain("data/live-export.db");
		expect(res.stdout).toContain("Size:");
		expect(dataFileSize("live-export.db")).toBeGreaterThan(0);
	});

	test("uses a timestamped filename in data/ by default", () => {
		const res = execHubCli(["export"]);
		expect(res.status, res.stderr).toBe(0);
		expect(res.stdout).toContain("Export Successful");

		const match = /data\/(hub-export-\d{8}-\d{6}\.db)/.exec(res.stdout);
		expect(match, res.stdout).not.toBeNull();
		const filename = match![1];
		expect(listDataFiles()).toContain(filename);
		expect(dataFileSize(filename)).toBeGreaterThan(0);
	});

	test("supports nested relative paths and absolute output paths", () => {
		const nested = execHubCli(["export", "--output", "backups/nested-export.db"]);
		expect(nested.status, nested.stderr).toBe(0);
		expect(nested.stdout).toContain("data/backups/nested-export.db");
		expect(dataFileSize("backups/nested-export.db")).toBeGreaterThan(0);

		const absolute = execHubCli(["export", "-o", "/app/data/abs-export.db"]);
		expect(absolute.status, absolute.stderr).toBe(0);
		expect(absolute.stdout).toContain("/app/data/abs-export.db");
		expect(dataFileSize("abs-export.db")).toBeGreaterThan(0);
	});

	test("refuses to overwrite an existing output file", () => {
		const res = execHubCli(["export", "-o", "live-export.db"]);
		expect(res.status).toBe(1);
		expect(res.stderr).toContain("output file already exists");
	});

	test("rejects demo mode and invalid configuration", () => {
		const demo = execHubCli(["export", "-o", "demo-export.db"], { env: { DEMO: "true" } });
		expect(demo.status).toBe(1);
		expect(demo.stderr).toContain("backup is not available in demo mode");

		const noConfig = runHubCli(["export", "-o", "no-config.db"], { mountData: true });
		expect(noConfig.status).toBe(1);
		expect(noConfig.stderr).toContain("failed to load hub configuration");
	});
});

test.describe("reset-passkeys", () => {
	test("validates flags, configuration, unknown users, demo mode and managed users", () => {
		const missing = execHubCli(["reset-passkeys"]);
		expect(missing.status).toBe(1);
		expect(missing.stderr).toContain("either --email or --id must be provided");

		const both = execHubCli(["reset-passkeys", "--email", LOCAL_USER_EMAIL, "--id", localUserId]);
		expect(both.status).toBe(1);
		expect(both.stderr).toContain("provide either --email or --id, not both");

		const unknownEmail = execHubCli(["reset-passkeys", "--email", "nobody@example.com"]);
		expect(unknownEmail.status).toBe(1);
		expect(unknownEmail.stderr).toContain('user not found with email "nobody@example.com"');

		const unknownId = execHubCli(["reset-passkeys", "--id", "nonexistent-user-id"]);
		expect(unknownId.status).toBe(1);
		expect(unknownId.stderr).toContain('user not found with id "nonexistent-user-id"');

		const demo = execHubCli(["reset-passkeys", "--email", LOCAL_USER_EMAIL], {
			env: { DEMO: "true" },
		});
		expect(demo.status).toBe(1);
		expect(demo.stderr).toContain("passkey recovery is not available in demo mode");

		const invalidConfig = runHubCli(["reset-passkeys", "--email", LOCAL_USER_EMAIL], {
			mountData: true,
		});
		expect(invalidConfig.status).toBe(1);
		expect(invalidConfig.stderr).toContain("failed to load hub configuration");

		const managed = execHubCli(["reset-passkeys", "--email", oidcUserEmail]);
		expect(managed.status).toBe(1);
		expect(managed.stderr).toContain("cannot reset passkeys for a managed user");
	});

	test("resets passkeys by email, revokes active sessions and credentials, and issues a working code", async ({
		browser,
	}) => {
		const existingSession = await openContext(browser, localUserCookies);
		try {
			expect((await cliApi(existingSession.page, "GET", "/auth/profile")).status).toBe(200);

			// Trimmed and case-insensitive email lookup while encrypted applications are present
			const res = execHubCli([
				"reset-passkeys",
				"--email",
				`  ${LOCAL_USER_EMAIL.toUpperCase()}  `,
			]);
			expect(res.status, res.stderr).toBe(0);
			expect(res.stdout).toContain("Passkey Recovery Ready");
			expect(res.stdout).toContain(localUserId);
			expect(res.stdout).toContain(LOCAL_USER_EMAIL);
			expect(res.stdout).toContain("24 hours");
			const code = extractEnrollmentCode(res.stdout);

			// Active session is revoked right away
			expect((await cliApi(existingSession.page, "GET", "/auth/profile")).status).toBe(401);

			// Old passkey credential no longer signs in
			const oldPasskeySession = await openContext(browser);
			try {
				await importCredential(oldPasskeySession.page, localUserCredential);
				await signInUi(oldPasskeySession.page);
				await expect(
					oldPasskeySession.page.getByText("passkey authentication failed"),
				).toBeVisible();
			} finally {
				await oldPasskeySession.context.close();
			}

			// Enrolling with the CLI code restores access and signs out / back in cleanly
			const recovered = await openContext(browser);
			try {
				localUserCredential = await enroll(recovered.page, code);
				expect((await cliApi(recovered.page, "GET", "/auth/profile")).body.id).toBe(localUserId);

				await signOutUi(recovered.page);
				await signInUi(recovered.page);
				await recovered.page.waitForURL("/");
				localUserCookies = (await recovered.context.storageState()).cookies;
			} finally {
				await recovered.context.close();
			}

			// Replaying the consumed enrollment code is rejected
			const replay = await openContext(browser);
			try {
				const replayRes = await cliApi(replay.page, "POST", "/auth/passkeys/register/begin", {
					enrollmentToken: code,
				});
				expect(replayRes.status).toBe(403);
			} finally {
				await replay.context.close();
			}
		} finally {
			await existingSession.context.close();
		}
	});

	test("resets passkeys by user ID and invalidates an earlier unused enrollment code", async ({
		browser,
	}) => {
		const first = execHubCli(["reset-passkeys", "--id", localUserId]);
		expect(first.status, first.stderr).toBe(0);
		const firstCode = extractEnrollmentCode(first.stdout);

		const second = execHubCli(["reset-passkeys", "--id", localUserId]);
		expect(second.status, second.stderr).toBe(0);
		const secondCode = extractEnrollmentCode(second.stdout);
		expect(secondCode).not.toBe(firstCode);

		const session = await openContext(browser);
		try {
			const stale = await cliApi(session.page, "POST", "/auth/passkeys/register/begin", {
				enrollmentToken: firstCode,
			});
			expect(stale.status).toBe(403);

			localUserCredential = await enroll(session.page, secondCode);
			expect((await cliApi(session.page, "GET", "/auth/profile")).body.id).toBe(localUserId);
			localUserCookies = (await session.context.storageState()).cookies;
		} finally {
			await session.context.close();
		}
	});
});

test.describe("import", () => {
	const BASELINE_BACKUP = "restore-baseline.db";

	test("rejects invalid backups and interactive cancellation without touching live data, then restores on confirmation", async ({
		browser,
	}) => {
		// Take a fresh baseline export that includes the re-enrolled local user passkey
		const exported = execHubCli(["export", "-o", BASELINE_BACKUP]);
		expect(exported.status, exported.stderr).toBe(0);

		// Mutate state after the backup so we can verify both non-destructive failures and full restore
		let disposableAgentId = "";
		let disposableUserId = "";
		const mutatedEmail = "cli-disposable@example.com";
		const renamedAgentName = "CLI Renamed Agent";
		const adminSession = await openContext(browser, adminCookies);
		try {
			const extraAgent = await cliApi(adminSession.page, "POST", "/agents", {
				name: "CLI Disposable Agent",
			});
			expect(extraAgent.status).toBe(201);
			disposableAgentId = (extraAgent.body as ApiAgent).id;

			const renamed = await cliApi(adminSession.page, "PUT", `/agents/${agent.id}`, {
				name: renamedAgentName,
			});
			expect(renamed.status).toBe(200);

			const extraUser = await cliApi(adminSession.page, "POST", "/admin/users", {
				name: "CLI Disposable User",
				email: mutatedEmail,
				roleIds: [USER_ROLE_ID],
			});
			expect(extraUser.status).toBe(201);
			disposableUserId = (extraUser.body as { id: string }).id;
		} finally {
			await adminSession.context.close();
		}

		stopCliHub();

		// Export also works offline while the hub is stopped
		const offlineExport = runHubCli(["export", "-o", "offline-export.db"], {
			mountData: true,
			env: envFor(SECRET_1),
		});
		expect(offlineExport.status, offlineExport.stderr).toBe(0);
		expect(dataFileSize("offline-export.db")).toBeGreaterThan(0);

		// Argument, file, demo mode and config validation
		const missingArg = runHubCli(["import"], { mountData: true, env: envFor(SECRET_1) });
		expect(missingArg.status).toBe(1);

		const missingFile = runHubCli(["import", "data/does-not-exist.db", "--yes"], {
			mountData: true,
			env: envFor(SECRET_1),
		});
		expect(missingFile.status).toBe(1);
		expect(missingFile.stderr).toContain("backup file not found");

		const demo = runHubCli(["import", `data/${BASELINE_BACKUP}`, "--yes"], {
			mountData: true,
			env: envFor(SECRET_1, { DEMO: "true" }),
		});
		expect(demo.status).toBe(1);
		expect(demo.stderr).toContain("import is not available in demo mode");

		const invalidConfig = runHubCli(["import", `data/${BASELINE_BACKUP}`, "--yes"], {
			mountData: true,
		});
		expect(invalidConfig.status).toBe(1);
		expect(invalidConfig.stderr).toContain("failed to load hub configuration");

		// Invalid, newer-schema and dirty backups are rejected before touching the live DB
		writeDataFile("corrupt-backup.db", "not a valid sqlite database");
		const corrupt = runHubCli(["import", "data/corrupt-backup.db", "--yes"], {
			mountData: true,
			env: envFor(SECRET_1),
		});
		expect(corrupt.status).toBe(1);
		expect(corrupt.stderr).toContain("import failed");

		writeModifiedBackup(
			BASELINE_BACKUP,
			"UPDATE schema_migrations SET version = version + 1",
			"newer-schema.db",
		);
		const newer = runHubCli(["import", "data/newer-schema.db", "--yes"], {
			mountData: true,
			env: envFor(SECRET_1),
		});
		expect(newer.status).toBe(1);
		expect(newer.stderr).toContain("newer OrcaCD");

		writeModifiedBackup(
			BASELINE_BACKUP,
			"UPDATE schema_migrations SET dirty = 1",
			"dirty-schema.db",
		);
		const dirty = runHubCli(["import", "data/dirty-schema.db", "--yes"], {
			mountData: true,
			env: envFor(SECRET_1),
		});
		expect(dirty.status).toBe(1);
		expect(dirty.stderr).toContain("dirty migration state");

		// Declining the interactive confirmation cancels without changing the DB
		const cancelled = runHubCli(["import", `data/${BASELINE_BACKUP}`], {
			mountData: true,
			env: envFor(SECRET_1),
			input: "no\n",
		});
		expect(cancelled.status, cancelled.stderr).toBe(0);
		expect(cancelled.stdout).toContain("WARNING");
		expect(cancelled.stdout).toContain("The hub server must be stopped before importing");
		expect(cancelled.stdout).toContain("Import cancelled");

		// Start the hub to verify none of the rejected/cancelled imports modified live state
		await startCliHub(SECRET_1);
		const checkMutated = await openContext(browser, adminCookies);
		try {
			const agents = (await cliApi(checkMutated.page, "GET", "/agents")).body as ApiAgent[];
			expect(agents.find((a) => a.id === disposableAgentId)).toBeDefined();
			expect(agents.find((a) => a.id === agent.id)?.name).toBe(renamedAgentName);

			const users = (await cliApi(checkMutated.page, "GET", "/admin/users")).body as ApiAdminUser[];
			expect(users.find((u) => u.id === disposableUserId)).toBeDefined();
		} finally {
			await checkMutated.context.close();
		}

		// Now stop the hub and confirm the interactive import
		stopCliHub();
		const restored = runHubCli(["import", `data/${BASELINE_BACKUP}`], {
			mountData: true,
			env: envFor(SECRET_1),
			input: "yes\n",
		});
		expect(restored.status, restored.stderr).toBe(0);
		expect(restored.stdout).toContain("WARNING");
		expect(restored.stdout).toContain("Import Successful");
		expect(restored.stdout).toContain(`data/${BASELINE_BACKUP}`);
		expect(restored.stdout).toContain("Database restored successfully");

		// Restore keeps a rollback backup of the replaced database
		expect(listDataFiles().some((file) => /^hub\.db-restore-\d+\.bak$/.test(file))).toBe(true);

		// Start the hub and verify pre-mutation state is fully restored
		await startCliHub(SECRET_1);
		const checkRestored = await openContext(browser, adminCookies);
		try {
			const agents = (await cliApi(checkRestored.page, "GET", "/agents")).body as ApiAgent[];
			expect(agents.find((a) => a.id === disposableAgentId)).toBeUndefined();
			expect(agents.find((a) => a.id === agent.id)?.name).toBe(AGENT_NAME);

			const users = (await cliApi(checkRestored.page, "GET", "/admin/users"))
				.body as ApiAdminUser[];
			expect(users.find((u) => u.id === disposableUserId)).toBeUndefined();

			const appRes = await cliApi(checkRestored.page, "GET", `/applications/${application.id}`);
			expect(appRes.status).toBe(200);
			expect((appRes.body as ApiApplication).name).toBe(APP_NAME);
			expect((appRes.body as ApiApplication).composeFile).toBe(COMPOSE_FILE);
		} finally {
			await checkRestored.context.close();
		}
	});
});

test.describe("key rotate", () => {
	test("validates secrets, demo mode, wrong old secret and interactive cancellation", () => {
		stopCliHub();

		const demo = runHubCli(["key", "rotate", "--old-secret", SECRET_1, "-y"], {
			mountData: true,
			env: envFor(SECRET_2, { DEMO: "true" }),
		});
		expect(demo.status).toBe(1);
		expect(demo.stderr).toContain("key rotation is not available in demo mode");

		const invalidConfig = runHubCli(["key", "rotate", "--old-secret", SECRET_1, "-y"], {
			mountData: true,
		});
		expect(invalidConfig.status).toBe(1);
		expect(invalidConfig.stderr).toContain("failed to load hub configuration");

		const missingOld = runHubCli(["key", "rotate", "-y"], {
			mountData: true,
			env: envFor(SECRET_2),
		});
		expect(missingOld.status).toBe(1);
		expect(missingOld.stderr).toContain("old secret is required");

		const shortOld = runHubCli(["key", "rotate", "--old-secret", "too-short", "-y"], {
			mountData: true,
			env: envFor(SECRET_2),
		});
		expect(shortOld.status).toBe(1);
		expect(shortOld.stderr).toContain("invalid old secret");

		const sameSecret = runHubCli(["key", "rotate", "--old-secret", SECRET_1, "-y"], {
			mountData: true,
			env: envFor(SECRET_1),
		});
		expect(sameSecret.status).toBe(1);
		expect(sameSecret.stderr).toContain("old and new secrets must be different");

		const wrongOld = runHubCli(["key", "rotate", "--old-secret", WRONG_SECRET, "-y"], {
			mountData: true,
			env: envFor(SECRET_2),
		});
		expect(wrongOld.status).toBe(1);
		expect(wrongOld.stderr).toContain("Check that the old secret matches");

		const cancelled = runHubCli(["key", "rotate", "--old-secret", SECRET_1], {
			mountData: true,
			env: envFor(SECRET_2),
			input: "no\n",
		});
		expect(cancelled.status, cancelled.stderr).toBe(0);
		expect(cancelled.stdout).toContain("WARNING");
		expect(cancelled.stdout).toContain("Key rotation cancelled");
	});

	test("re-encrypts all encrypted models, recomputes name hashes, invalidates old tokens and keeps everything working under the new secret", async ({
		browser,
	}) => {
		// Rotate SECRET_1 -> SECRET_2 using OLD_APP_SECRET and interactive "yes" confirmation
		const rotated = runHubCli(["key", "rotate"], {
			mountData: true,
			env: envFor(SECRET_2, { OLD_APP_SECRET: SECRET_1 }),
			input: "yes\n",
		});
		expect(rotated.status, rotated.stderr).toBe(0);
		expect(rotated.stdout).toContain("Key Rotation Successful");
		expect(rotated.stdout).toContain("Rows re-encrypted:");
		expect(rotated.stdout).toContain("Agents: 1");
		expect(rotated.stdout).toContain("Applications: 1");
		expect(rotated.stdout).toContain("Repositories: 1");
		expect(rotated.stdout).toContain("Notifications: 1");
		expect(rotated.stdout).toContain("Passkeys: 2");
		expect(rotated.stdout).toContain("OIDC providers: 1");
		expect(rotated.stdout).toContain("Restart the hub with the new APP_SECRET.");

		// Starting the hub with the old APP_SECRET must fail to open the rotated database
		const oldStart = runHubCli([], {
			mountData: true,
			env: envFor(SECRET_1),
			timeoutMs: 15_000,
		});
		expect(oldStart.status).not.toBe(0);
		expect(`${oldStart.stdout}\n${oldStart.stderr}`).toContain("failed to connect to database");

		// Start the hub with the new APP_SECRET
		await startCliHub(SECRET_2);

		// Session cookies signed with the old APP_SECRET are rejected
		const staleSession = await openContext(browser, adminCookies);
		try {
			expect((await cliApi(staleSession.page, "GET", "/auth/profile")).status).toBe(401);
		} finally {
			await staleSession.context.close();
		}

		// Existing passkeys still work after rotation (passkeys.credential was re-encrypted)
		let adminSession: { context: BrowserContext; page: Page } | undefined;
		try {
			adminSession = await signInWithPasskey(browser, adminCredential);
			expect((await cliApi(adminSession.page, "GET", "/auth/profile")).body.email).toBe(
				ADMIN_EMAIL,
			);
			adminCookies = (await adminSession.context.storageState()).cookies;

			// Agent name decrypts; old agent JWT is rejected and a re-issued token passes auth
			const agentRes = await cliApi(adminSession.page, "GET", `/agents/${agent.id}`);
			expect(agentRes.status).toBe(200);
			expect((agentRes.body as ApiAgent).name).toBe(AGENT_NAME);

			const oldWsAuth = await fetch(`${CLI_URL}/api/v1/ws`, {
				headers: { Authorization: jwtFromAgentToken(agentAuthToken) },
			});
			expect(oldWsAuth.status).toBe(401);

			const reissued = await cliApi(adminSession.page, "POST", `/agents/${agent.id}/rotate-token`);
			expect(reissued.status).toBe(200);
			const newAgentToken = (reissued.body as { authToken: string }).authToken;
			expect(newAgentToken).toBeTruthy();

			// Authenticated WS request without Upgrade headers reaches the WebSocket upgrader (400), not 401
			const newWsAuth = await fetch(`${CLI_URL}/api/v1/ws`, {
				headers: { Authorization: jwtFromAgentToken(newAgentToken) },
			});
			expect(newWsAuth.status).toBe(400);

			// Repository auth_token decrypts (authenticated sync against Gitea mock succeeds)
			const requestsBefore = (await requests(REPO_NAME)).length;
			const sync = await cliApi(adminSession.page, "POST", `/repositories/${repository.id}/sync`);
			expect(sync.status).toBe(202);
			await expect
				.poll(
					async () =>
						(await requests(REPO_NAME)).slice(requestsBefore).some((entry) => entry.authorized),
					{ timeout: 15_000 },
				)
				.toBe(true);
			await expect
				.poll(
					async () =>
						(
							(await cliApi(adminSession!.page, "GET", `/repositories/${repository.id}`))
								.body as ApiRepository
						).syncStatus,
					{ timeout: 15_000 },
				)
				.toBe("success");

			// Repository webhook_secret decrypts
			const validRepoHook = await fetch(
				`${CLI_URL}/api/v1/webhooks/repositories/${repository.id}`,
				{
					method: "POST",
					headers: { Authorization: `Bearer ${repoWebhookSecret}` },
				},
			);
			expect(validRepoHook.status).toBe(204);

			const invalidRepoHook = await fetch(
				`${CLI_URL}/api/v1/webhooks/repositories/${repository.id}`,
				{
					method: "POST",
					headers: { Authorization: "Bearer wrong-secret" },
				},
			);
			expect(invalidRepoHook.status).toBe(401);

			// Application name, compose_file, image_webhook_secret and recomputed name_hash work
			const appRes = await cliApi(adminSession.page, "GET", `/applications/${application.id}`);
			expect(appRes.status).toBe(200);
			expect((appRes.body as ApiApplication).name).toBe(APP_NAME);
			expect((appRes.body as ApiApplication).composeFile).toBe(COMPOSE_FILE);

			const validImageHook = await fetch(`${CLI_URL}/api/v1/webhooks/images/${application.id}`, {
				method: "POST",
				headers: { Authorization: `Bearer ${imageWebhookSecret}` },
			});
			expect(validImageHook.status).toBe(204);

			const invalidImageHook = await fetch(`${CLI_URL}/api/v1/webhooks/images/${application.id}`, {
				method: "POST",
				headers: { Authorization: "Bearer wrong-secret" },
			});
			expect(invalidImageHook.status).toBe(401);

			// Blind index name_hash was recomputed under SECRET_2, so duplicate name detection still fires
			const duplicateApp = await cliApi(adminSession.page, "POST", "/applications", {
				name: `  ${APP_NAME.toLowerCase()}  `,
				agentId: agent.id,
				repositoryId: repository.id,
				branch: "main",
				path: "compose.yaml",
			});
			expect(duplicateApp.status).toBe(409);

			// Notification name and config (with secret header) decrypt and deliver
			await clearRequests(NOTIFY_HOOK);
			const testNotify = await cliApi(
				adminSession.page,
				"POST",
				`/notifications/${notificationId}/test`,
			);
			expect(testNotify.status).toBe(200);
			await waitForMessages(NOTIFY_HOOK, 1);
			const [delivered] = await receivedRequests(NOTIFY_HOOK);
			expect(delivered.headers.authorization).toBe(NOTIFY_AUTH_HEADER);
		} finally {
			await adminSession?.context.close();
		}

		// OIDC provider client_secret decrypts and completes a real sign-in against the mock IdP
		const oidcSession = await openContext(browser);
		try {
			await clickProvider(oidcSession.page, oidcProvider);
			await signInAtMock(oidcSession.page, oidcUserSub);
			await oidcSession.page.waitForURL("/");
			expect((await cliApi(oidcSession.page, "GET", "/auth/profile")).body.email).toBe(
				oidcUserEmail,
			);
		} finally {
			await oidcSession.context.close();
		}
	});

	test("supports repeated rotation with --old-secret and -y", async ({ browser }) => {
		stopCliHub();

		const rotatedAgain = runHubCli(["key", "rotate", "--old-secret", SECRET_2, "-y"], {
			mountData: true,
			env: envFor(SECRET_3),
		});
		expect(rotatedAgain.status, rotatedAgain.stderr).toBe(0);
		expect(rotatedAgain.stdout).toContain("Key Rotation Successful");

		await startCliHub(SECRET_3);

		const session = await signInWithPasskey(browser, localUserCredential);
		try {
			expect((await cliApi(session.page, "GET", "/auth/profile")).body.id).toBe(localUserId);
		} finally {
			await session.context.close();
		}
	});

	test("import replaces a database encrypted with another secret and explains secret mismatches", async ({
		browser,
	}) => {
		stopCliHub();

		// Live DB is currently encrypted with SECRET_3, while restore-baseline.db is encrypted with SECRET_1
		const crossSecretImport = runHubCli(["import", "/app/data/restore-baseline.db", "--yes"], {
			mountData: true,
			env: envFor(SECRET_1),
		});
		expect(crossSecretImport.status, crossSecretImport.stderr).toBe(0);
		expect(crossSecretImport.stdout).toContain("Import Successful");

		await waitForNextSecond();

		// Importing a SECRET_1 backup while APP_SECRET is SECRET_2 explains the APP_SECRET mismatch
		const wrongSecretImport = runHubCli(["import", "/app/data/restore-baseline.db", "--yes"], {
			mountData: true,
			env: envFor(SECRET_2),
		});
		expect(wrongSecretImport.status).toBe(1);
		expect(wrongSecretImport.stderr).toContain(
			"Check that APP_SECRET matches the one the backup was encrypted with",
		);

		await waitForNextSecond();

		// Restore cleanly under SECRET_1 and verify the hub boots and serves the restored data
		const finalRestore = runHubCli(["import", "/app/data/restore-baseline.db", "-y"], {
			mountData: true,
			env: envFor(SECRET_1),
		});
		expect(finalRestore.status, finalRestore.stderr).toBe(0);

		await startCliHub(SECRET_1);
		const session = await signInWithPasskey(browser, adminCredential);
		try {
			const appRes = await cliApi(session.page, "GET", `/applications/${application.id}`);
			expect(appRes.status).toBe(200);
			expect((appRes.body as ApiApplication).name).toBe(APP_NAME);
		} finally {
			await session.context.close();
		}
	});
});
