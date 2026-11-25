import { test, expect, type Page } from "@playwright/test";

const APP_ID = "monitoring-app";
const DAY = 24 * 60 * 60 * 1000;

function container(overrides: Record<string, unknown> = {}) {
	return {
		id: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		name: "shop-app-1",
		service: "app",
		containerNumber: 1,
		state: "running",
		health: "healthy",
		healthRaw: "healthy",
		healthFailingStreak: 0,
		image: "ghcr.io/example/shop:1.4.2",
		imageId: "sha256:0123456789abcdef",
		imageDigest: "ghcr.io/example/shop@sha256:feedface",
		restartCount: 2,
		restartPolicy: "unless-stopped",
		exitCode: 0,
		oomKilled: false,
		error: "",
		logDriver: "json-file",
		cpuLimit: 0,
		createdAt: new Date(Date.now() - 13 * DAY).toISOString(),
		startedAt: new Date(Date.now() - 12 * DAY - 3 * 60 * 60 * 1000).toISOString(),
		finishedAt: null,
		ports: [
			{ ip: "0.0.0.0", publicPort: 8080, privatePort: 3000, protocol: "tcp" },
			{ ip: "::", publicPort: 8080, privatePort: 3000, protocol: "tcp" },
			{ ip: "", publicPort: 0, privatePort: 9229, protocol: "tcp" },
		],
		networks: [{ name: "shop_default", ipAddress: "172.18.0.3", aliases: ["app"] }],
		mounts: [
			{ type: "volume", name: "shop_data", destination: "/data", readOnly: false },
			{ type: "bind", name: "", destination: "/etc/app.conf", readOnly: true },
		],
		...overrides,
	};
}

const database = container({
	id: "b1b2c3d4e5f6b1b2c3d4e5f6b1b2c3d4e5f6b1b2c3d4e5f6b1b2c3d4e5f6b1b2",
	name: "shop-db-1",
	service: "db",
	image: "postgres:17",
	ports: [],
	mounts: [],
	restartCount: 0,
});

async function mockApplication(page: Page) {
	await page.route(`**/api/v1/applications/${APP_ID}`, (route) =>
		route.fulfill({
			json: {
				id: APP_ID,
				name: "Shop",
				icon: "",
				syncStatus: "synced",
				healthStatus: "healthy",
				agentId: "monitoring-agent",
				agentName: "Monitoring agent",
			},
		}),
	);
}

const STATS_STREAM = `**/api/v1/applications/${APP_ID}/containers/*/stats/stream`;

function sample(overrides: Record<string, unknown> = {}) {
	return {
		timestampMs: 1_700_000_000_000,
		cpuPercent: 200,
		memUsageBytes: 256 * 1024 * 1024,
		memLimitBytes: 1024 * 1024 * 1024,
		netRxBytes: 1_000,
		netTxBytes: 500,
		pids: 4,
		onlineCpus: 4,
		...overrides,
	};
}

async function mockContainers(page: Page, status: number, body: unknown) {
	await mockApplication(page);
	await page.route(`**/api/v1/applications/${APP_ID}/containers`, (route) =>
		route.fulfill({ status, json: body }),
	);
	await page.route(STATS_STREAM, (route) =>
		route.fulfill({ status: 404, json: { error: "container not found" } }),
	);
}

/** Ends with the container being gone, so the page doesn't reconnect and repeat the samples. */
async function mockStats(page: Page, samples: Record<string, unknown>[]) {
	const events = [
		...samples.map((entry) => `event: stats\ndata: ${JSON.stringify(entry)}\n\n`),
		`event: end\ndata: ${JSON.stringify({ reason: "container_gone", error: "" })}\n\n`,
	];
	await page.route(STATS_STREAM, (route) =>
		route.fulfill({ status: 200, contentType: "text/event-stream", body: events.join("") }),
	);
}

const card = (page: Page, title: string) =>
	page.locator("[data-slot='card']").filter({ has: page.getByText(title, { exact: true }) });

test("the sidebar links to the monitoring page", async ({ page }) => {
	await mockContainers(page, 200, { containers: [container()] });
	await page.goto(`/applications/${APP_ID}/details`);

	await page.locator("[data-sidebar='menu-button']").filter({ hasText: "Monitoring" }).click();

	await expect(page).toHaveURL(new RegExp(`/applications/${APP_ID}/monitoring`));
	await expect(page.getByRole("heading", { name: "Monitoring" })).toBeVisible();
	const breadcrumb = page.getByRole("navigation", { name: "breadcrumb" });
	await expect(breadcrumb.getByRole("link", { name: "Shop" })).toBeVisible();
	await expect(breadcrumb.getByText("Monitoring", { exact: true })).toBeVisible();
});

test("shows state, uptime, restarts, ports and details of a container", async ({ page }) => {
	await mockContainers(page, 200, { containers: [container()] });
	await page.goto(`/applications/${APP_ID}/monitoring`);

	await expect(page.getByText("Running · Up 12d 3h")).toBeVisible();
	await expect(page.locator("[data-slot='badge']").filter({ hasText: /^Healthy$/ })).toBeVisible();

	const restarts = page.locator("[data-slot='card']").filter({ hasText: "Restarts" });
	await expect(restarts.getByText("2", { exact: true })).toBeVisible();

	const ports = page.locator("[data-slot='card']").filter({ hasText: "Ports" });
	await expect(ports.getByText("8080")).toHaveCount(1);
	await expect(ports.getByText("3000/tcp")).toBeVisible();
	await expect(ports.getByText("9229/tcp")).toBeVisible();
	await expect(ports.getByText("not published")).toBeVisible();

	const details = page.locator("[data-slot='card']").filter({ hasText: "Digest" });
	await expect(details.getByText("ghcr.io/example/shop:1.4.2")).toBeVisible();
	await expect(details.getByText("ghcr.io/example/shop@sha256:feedface")).toBeVisible();
	await expect(details.getByText("a1b2c3d4e5f6", { exact: true })).toBeVisible();
	await expect(details.getByText("shop_default")).toBeVisible();
	await expect(details.getByText("172.18.0.3")).toBeVisible();
	await expect(details.getByText("shop_data")).toBeVisible();
	await expect(details.getByText("/etc/app.conf")).toBeVisible();
});

test("the switcher selects a container and keeps it in the URL", async ({ page }) => {
	await mockContainers(page, 200, { containers: [container(), database] });
	await page.goto(`/applications/${APP_ID}/monitoring`);

	const switcher = page.getByRole("tablist", { name: "Container" });
	await expect(switcher.getByRole("tab", { name: "app" })).toHaveAttribute("aria-selected", "true");
	await expect(page.getByText("ghcr.io/example/shop:1.4.2")).toBeVisible();

	await switcher.getByRole("tab", { name: "db" }).click();
	await expect(page).toHaveURL(/container=db/);
	await expect(page.getByText("postgres:17")).toBeVisible();
	await expect(page.getByText("No ports exposed")).toBeVisible();

	await page.reload();
	await expect(switcher.getByRole("tab", { name: "db" })).toHaveAttribute("aria-selected", "true");
});

test("scaled services are told apart by their replica number", async ({ page }) => {
	const first = container({ containerNumber: 1 });
	const second = container({
		id: "c1b2c3d4e5f6c1b2c3d4e5f6c1b2c3d4e5f6c1b2c3d4e5f6c1b2c3d4e5f6c1b2",
		name: "shop-app-2",
		containerNumber: 2,
	});
	await mockContainers(page, 200, { containers: [first, second] });
	await page.goto(`/applications/${APP_ID}/monitoring`);

	const switcher = page.getByRole("tablist", { name: "Container" });
	await expect(switcher.getByRole("tab", { name: "app #1" })).toBeVisible();
	await switcher.getByRole("tab", { name: "app #2" }).click();
	await expect(page.getByText("c1b2c3d4e5f6", { exact: true })).toBeVisible();
});

test("an exited container shows when it stopped, why, and the exit code", async ({ page }) => {
	await mockContainers(page, 200, {
		containers: [
			container({
				state: "exited",
				health: "stopped",
				exitCode: 137,
				oomKilled: true,
				error: "",
				finishedAt: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
			}),
		],
	});
	await page.goto(`/applications/${APP_ID}/monitoring`);

	await expect(page.getByText(/^Exited · /)).toBeVisible();
	await expect(page.getByText("Last exit code 137")).toBeVisible();
	await expect(page.getByText("Out of memory")).toBeVisible();
});

test("explains that the agent is offline", async ({ page }) => {
	await mockContainers(page, 503, { error: "The agent is offline." });
	await page.goto(`/applications/${APP_ID}/monitoring`);

	await expect(page.getByText("Agent is offline")).toBeVisible();
	await expect(page.locator("[data-slot='card']").filter({ hasText: "Restarts" })).toHaveCount(0);
});

test("explains that the agent needs an update", async ({ page }) => {
	await mockContainers(page, 422, { error: "This agent does not support container monitoring." });
	await page.goto(`/applications/${APP_ID}/monitoring`);

	await expect(page.getByText("Agent update required")).toBeVisible();
	await expect(page.getByText("Update the agent", { exact: false })).toBeVisible();
});

test("shows an empty state without containers", async ({ page }) => {
	await mockContainers(page, 200, { containers: [] });
	await page.goto(`/applications/${APP_ID}/monitoring`);

	await expect(page.getByRole("heading", { name: "No containers" })).toBeVisible();
});

test("shows the error of a failed request", async ({ page }) => {
	await mockContainers(page, 502, { error: "The agent failed to list the containers." });
	await page.goto(`/applications/${APP_ID}/monitoring`);

	await expect(page.getByText("Failed to load the containers")).toBeVisible();
	await expect(page.getByText("The agent failed to list the containers.")).toBeVisible();
});

test("the cards show the CPU, memory and network values with their charts", async ({ page }) => {
	await mockContainers(page, 200, { containers: [container()] });
	await mockStats(page, [
		sample(),
		sample({ timestampMs: 1_700_000_002_000, netRxBytes: 5_096, netTxBytes: 3_572 }),
		sample({ timestampMs: 1_700_000_004_000, netRxBytes: 9_192, netTxBytes: 6_644 }),
	]);
	await page.goto(`/applications/${APP_ID}/monitoring`);

	const cpu = card(page, "CPU");
	await expect(cpu.locator("[data-slot='card-title']")).toHaveText("50%");
	await expect(cpu.getByText("of 4 CPUs")).toBeVisible();

	const memory = card(page, "Memory");
	await expect(memory.locator("[data-slot='card-title']")).toHaveText("256 MiB");
	await expect(memory.getByText("of 1 GiB")).toBeVisible();

	const network = card(page, "Network");
	await expect(network.locator("[data-slot='card-title']")).toHaveText("↓ 2 KiB/s");
	await expect(network.getByText("↑ 1.5 KiB/s")).toBeVisible();
	const total = network.locator("[data-slot='card-action']");
	await expect(total).toContainText("Total");
	await expect(total.getByText("↓ 9 KiB", { exact: true })).toBeVisible();
	await expect(total.getByText("↑ 6.5 KiB", { exact: true })).toBeVisible();

	for (const stat of [cpu, memory, network]) {
		// The mocked samples give flat lines, which have no height and count as hidden.
		await expect(stat.locator("[data-slot='chart'] .recharts-area-curve").first()).toBeAttached();
		await expect(stat.getByText("Container stopped, showing the last values")).toBeVisible();
	}
});

test("hovering a chart shows the values of that moment", async ({ page }) => {
	await mockContainers(page, 200, { containers: [container()] });
	await mockStats(page, [
		sample(),
		sample({ timestampMs: 1_700_000_002_000, netRxBytes: 5_096, netTxBytes: 3_572 }),
		sample({ timestampMs: 1_700_000_004_000, netRxBytes: 9_192, netTxBytes: 6_644 }),
	]);
	await page.goto(`/applications/${APP_ID}/monitoring`);

	const chart = card(page, "Network").locator("[data-slot='chart']");
	await expect(chart.locator(".recharts-area-curve").first()).toBeAttached();
	// The samples only cover the last pixels of the time window and the tooltip only shows there.
	await chart.locator(".recharts-area-area").first().hover();

	const tooltip = page.locator(".recharts-tooltip-wrapper");
	await expect(tooltip.getByText("Received")).toBeVisible();
	await expect(tooltip.getByText("Sent")).toBeVisible();
	await expect(tooltip.getByText("2 KiB/s")).toBeVisible();
	await expect(tooltip.getByText("1.5 KiB/s")).toBeVisible();
});

test("the CPU is shown relative to the limit of the container", async ({ page }) => {
	await mockContainers(page, 200, { containers: [container({ cpuLimit: 0.5 })] });
	await mockStats(page, [sample({ cpuPercent: 25 })]);
	await page.goto(`/applications/${APP_ID}/monitoring`);

	const cpu = card(page, "CPU");
	await expect(cpu.locator("[data-slot='card-title']")).toHaveText("50%");
	await expect(cpu.getByText("of 0.5 CPU")).toBeVisible();
});

test("a container that is not running doesn't open a stats stream", async ({ page }) => {
	const requests: string[] = [];
	page.on("request", (request) => {
		if (request.url().includes("/stats/stream")) {
			requests.push(request.url());
		}
	});
	await mockContainers(page, 200, {
		containers: [
			container({
				state: "exited",
				health: "stopped",
				finishedAt: new Date(Date.now() - 60_000).toISOString(),
			}),
		],
	});
	await page.goto(`/applications/${APP_ID}/monitoring`);

	await expect(card(page, "CPU").getByText("Container is not running")).toBeVisible();
	await expect(card(page, "CPU").locator("[data-slot='card-title']")).toHaveText("—");
	expect(requests).toEqual([]);
});

test("the cards wait for the first sample", async ({ page }) => {
	await mockContainers(page, 200, { containers: [container()] });
	await page.goto(`/applications/${APP_ID}/monitoring`);

	await expect(card(page, "Memory").getByText("Waiting for data…")).toBeVisible();
	await expect(card(page, "Memory").locator("[data-slot='card-title']")).toHaveText("—");
});
