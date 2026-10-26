import { test, expect, type Browser, type Page } from "@playwright/test";
import { APP_URL, HUB_WS_URL } from "./const.ts";
import { api } from "./helpers/api.ts";
import { gotoApp } from "./helpers/navigation.ts";
import { newAuthenticatedContext } from "./helpers/setup.ts";
import {
	agentHealthcheck,
	agentLogs,
	agentState,
	duplicateAgentLogs,
	removeDuplicateAgent,
	restartHub,
	startAgent,
	startDuplicateAgent,
	stopAgent,
	stopAgentInBackground,
} from "./helpers/agent-container.ts";

interface ApiAgent {
	id: string;
	name: string;
	status: string;
}

const AGENT_NAME = "E2E Agent";

test.describe.configure({ mode: "serial" });

let authToken = "";
let agentId = "";

async function listAgents(page: Page) {
	if (!page.url().startsWith(APP_URL)) {
		await page.goto("/agents");
	}
	const { ok, body } = await page.evaluate(async () => {
		const response = await fetch("/api/v1/agents");
		return { ok: response.ok, body: await response.json() };
	});
	expect(ok).toBeTruthy();
	return body as ApiAgent[];
}

async function agentStatus(page: Page) {
	return (await listAgents(page)).find((agent) => agent.id === agentId)?.status;
}

function agentCard(page: Page) {
	return page.locator("[data-slot='card']", { hasText: AGENT_NAME });
}

// Reloads and retries instead of waiting once, the page can be rendered before the hub is fully back
async function openAgentCard(page: Page) {
	await expect(async () => {
		await page.goto("/agents");
		await expect(agentCard(page)).toBeVisible({ timeout: 5_000 });
	}).toPass({ timeout: 30_000 });
}

async function expectBadge(page: Page, label: "Online" | "Offline") {
	await expect(async () => {
		await page.goto("/agents");
		await expect(agentCard(page).getByText(label, { exact: true })).toBeVisible({
			timeout: 5_000,
		});
	}).toPass({ timeout: 30_000 });
}

// A retried serial group starts over with the state of the failed attempt still in place
async function removeLeftovers(browser: Browser) {
	removeDuplicateAgent();
	stopAgent();
	authToken = "";
	agentId = "";

	const context = await newAuthenticatedContext(browser);
	try {
		const page = await context.newPage();
		for (const agent of await listAgents(page)) {
			await api(page, "DELETE", `/agents/${agent.id}`);
		}
	} finally {
		await context.close();
	}
}

test.beforeAll(async ({ browser }) => {
	await removeLeftovers(browser);
});

test.afterAll(async ({ browser }) => {
	await removeLeftovers(browser);
});

test.describe("Agent with invalid configuration", () => {
	test("missing HUB_URL and AUTH_TOKEN", async ({ page }) => {
		startAgent({});

		await expect.poll(agentLogs, { timeout: 30_000 }).toContain("HUB_URL is not set");
		// Fatal config errors keep the container alive so the error stays visible
		expect(agentState().status).toBe("running");
		expect(agentHealthcheck()).toBe(false);
		expect(await listAgents(page)).toHaveLength(0);
	});

	test("malformed AUTH_TOKEN", async ({ page }) => {
		startAgent({ hubUrl: HUB_WS_URL, authToken: "not-a-valid-token" });

		await expect.poll(agentLogs, { timeout: 30_000 }).toContain("AUTH_TOKEN:");
		expect(agentState().status).toBe("running");
		expect(await listAgents(page)).toHaveLength(0);
	});

	test("unsupported HUB_URL scheme", async () => {
		startAgent({ hubUrl: "ftp://hub:8080", authToken: "not-a-valid-token" });

		await expect.poll(() => agentState().status, { timeout: 30_000 }).toBe("exited");
		expect(agentState().exitCode).toBe(1);
	});
});

test.describe("Agent setup", () => {
	test("add agent in the UI", async ({ page }) => {
		await gotoApp(page, "/agents");

		await page.getByRole("button", { name: "Add Agent" }).click();
		const dialog = page.getByRole("dialog");
		await dialog.getByRole("textbox", { name: "Name" }).fill(AGENT_NAME);
		await dialog.getByRole("button", { name: "Add Agent" }).click();

		const setup = page.getByRole("dialog", { name: `Connect agent “${AGENT_NAME}”` });
		await expect(
			setup.getByText(`Waiting for the first connection from ${AGENT_NAME}`),
		).toBeVisible();

		const compose = await setup.locator("pre").innerText();
		expect(compose).toContain(`HUB_URL: ${APP_URL}`);
		expect(compose).toMatch(/image: ghcr\.io\/orcacd\/agent:\S+/);
		authToken = compose.match(/AUTH_TOKEN: (\S+)/)?.[1] ?? "";
		expect(authToken).not.toBe("");
		await setup.getByRole("button", { name: "Close" }).click();

		const agents = await listAgents(page);
		expect(agents).toHaveLength(1);
		agentId = agents[0].id;

		await expect(agentCard(page)).toBeVisible();
		await expectBadge(page, "Offline");
	});

	test("stays offline when the hub is unreachable", async ({ page }) => {
		startAgent({ hubUrl: "ws://does-not-exist:8080/api/v1/ws", authToken });

		await expect.poll(agentLogs, { timeout: 30_000 }).toContain("connection failed, retrying");
		expect(await agentStatus(page)).toBe("offline");
	});

	test("connects with the correct configuration", async ({ page }) => {
		startAgent({ hubUrl: HUB_WS_URL, authToken });

		await expect.poll(() => agentStatus(page), { timeout: 30_000 }).toBe("online");
		expect(agentLogs()).toContain("connected to hub");
		await expect.poll(agentHealthcheck, { timeout: 30_000 }).toBe(true);
		await expectBadge(page, "Online");
	});

	test("goes offline right away when the agent stops", async ({ page }) => {
		await openAgentCard(page);
		await expect(agentCard(page).getByText("Online", { exact: true })).toBeVisible();

		const stopped = stopAgentInBackground();

		// No reload: the badge flips through the SSE update. A closed socket is noticed
		// immediately, the hub's 90s read deadline would be far outside this timeout.
		await expect(agentCard(page).getByText("Offline", { exact: true })).toBeVisible({
			timeout: 10_000,
		});
		await stopped;

		expect(await agentStatus(page)).toBe("offline");
		await expectBadge(page, "Offline");
	});
});

test.describe("Agent token rotation", () => {
	test("rotated token replaces the old one", async ({ page }) => {
		const oldToken = authToken;

		await openAgentCard(page);
		await agentCard(page).getByRole("button", { name: "Card actions" }).click();

		await page.getByRole("menu").getByRole("button", { name: "Rotate token" }).click();
		await page.getByRole("dialog").getByRole("button", { name: "Rotate token" }).click();

		const token = page.locator(`#agent-auth-token-${agentId}`);
		await expect(token).toBeVisible();
		authToken = await token.inputValue();
		expect(authToken).not.toBe(oldToken);
		await page.getByRole("button", { name: "Done" }).click();

		// The hub rejects the old token and the agent aborts
		startAgent({ hubUrl: HUB_WS_URL, authToken: oldToken });
		await expect.poll(() => agentState().status, { timeout: 30_000 }).toBe("exited");
		expect(agentState().exitCode).toBe(1);
		expect(agentLogs()).toContain("unauthorized");
		expect(await agentStatus(page)).toBe("offline");

		startAgent({ hubUrl: HUB_WS_URL, authToken });
		await expect.poll(() => agentStatus(page), { timeout: 30_000 }).toBe("online");
	});
});

// The agent is online with the rotated token from here on
test.describe("Agent resilience", () => {
	test("reconnects after the hub restarts", async ({ page }) => {
		await restartHub();

		await expect.poll(agentLogs, { timeout: 30_000 }).toContain("reconnecting");
		await expect.poll(() => agentStatus(page), { timeout: 60_000 }).toBe("online");
		await expectBadge(page, "Online");
		await expect.poll(agentHealthcheck, { timeout: 30_000 }).toBe(true);
	});

	test("a second connection with the same token is rejected", async ({ page }) => {
		startDuplicateAgent({ hubUrl: HUB_WS_URL, authToken });

		await expect.poll(duplicateAgentLogs, { timeout: 30_000 }).toContain("connection failed");
		expect(duplicateAgentLogs()).toContain("bad handshake");
		// The duplicate keeps retrying, the original connection is not affected
		expect(await agentStatus(page)).toBe("online");
		expect(agentState().status).toBe("running");

		removeDuplicateAgent();
		expect(await agentStatus(page)).toBe("online");
	});

	test("agent stops when it is deleted in the UI", async ({ page }) => {
		await expect.poll(() => agentStatus(page), { timeout: 30_000 }).toBe("online");
		await openAgentCard(page);
		await agentCard(page).getByRole("button", { name: "Card actions" }).click();
		await page.getByRole("menu").getByRole("button", { name: "Delete" }).click();
		await page.getByRole("dialog").getByRole("button", { name: "Proceed" }).click();

		await expect(agentCard(page)).toHaveCount(0);
		expect(await listAgents(page)).toHaveLength(0);

		// The hub drops the connection, the reconnect is rejected as the token is no longer valid
		await expect.poll(() => agentState().status, { timeout: 60_000 }).toBe("exited");
		expect(agentState().exitCode).toBe(1);
		expect(agentLogs()).toContain("unauthorized");
	});
});
