import { test, expect, type Page } from "@playwright/test";
import { api, seedRepository, type ApiRepository } from "./helpers/api.ts";
import { commit, deleteRepo } from "./helpers/gitea-mock.ts";
import { gotoApp } from "./helpers/navigation.ts";
import {
	createNotification,
	deleteNotifications,
	getNotification,
} from "./helpers/notifications.ts";
import { uniqueId } from "./helpers/oidc.ts";
import { hookUrl } from "./helpers/receiver-mock.ts";

const services = "services:\n  web:\n    image: busybox:1.37\n";
const managedCompose = `x-orcacd:
  delete-old-images: true
  rollback:
    enabled: true
    health-timeout: 2m
${services}`;

let repo: ApiRepository | undefined;
let agentId = "";
let appId = "";

interface ApiApplication {
	commit: string;
	lastSyncError?: string;
	deleteOldImages: boolean;
	rollback: { enabled: boolean; healthTimeoutSeconds: number };
	managedSettings: string[];
}

async function getApplication(page: Page) {
	return (await api(page, "GET", `/applications/${appId}`)).body as ApiApplication;
}

async function syncRepository(page: Page) {
	const sync = await api(page, "POST", `/repositories/${repo!.id}/sync`);
	expect(sync.status, JSON.stringify(sync.body)).toBeLessThan(300);
}

test.describe("Settings from the compose file", () => {
	test.beforeEach(async ({ page }) => {
		repo = await seedRepository(
			page,
			`e2e/compose-config-${uniqueId("repo")}`,
			{},
			{ branches: { main: { files: { "compose.yaml": managedCompose } } } },
		);
		// The agent never connects; the application only needs it to exist.
		const agent = await api(page, "POST", "/agents", { name: uniqueId("Compose Config Agent") });
		expect(agent.status).toBe(201);
		agentId = agent.body.id as string;
		const created = await api(page, "POST", "/applications", {
			name: uniqueId("Compose Config App"),
			repositoryId: repo.id,
			agentId,
			branch: "main",
			path: "compose.yaml",
		});
		expect(created.status, JSON.stringify(created.body)).toBe(201);
		appId = created.body.id as string;
	});

	test.afterEach(async ({ page }) => {
		await api(page, "DELETE", `/applications/${appId}`);
		await api(page, "DELETE", `/agents/${agentId}`);
		if (repo) {
			await api(page, "DELETE", `/repositories/${repo.id}`);
			await deleteRepo(repo.name);
		}
	});

	test("are applied and read-only", async ({ page }) => {
		expect(await getApplication(page)).toMatchObject({
			deleteOldImages: true,
			rollback: { enabled: true, healthTimeoutSeconds: 120 },
			managedSettings: ["deleteOldImages", "rollback.enabled", "rollback.healthTimeoutSeconds"],
		});

		await gotoApp(page, `/applications/${appId}/settings/rollback`);
		const rollback = page.getByRole("switch", { name: "Automatic rollback" });
		await expect(rollback).toBeChecked();
		await expect(rollback).toBeDisabled();
		await expect(page.getByRole("checkbox", { name: "Deploy fails" })).toBeEnabled();
		await expect(page.getByText("Set in the compose file with")).toHaveCount(2);

		await gotoApp(page, `/applications/${appId}/settings/general`);
		await expect(page.getByRole("switch", { name: /Delete old images/ })).toBeDisabled();

		const update = await api(page, "PUT", `/applications/${appId}/rollback/settings`, {
			enabled: false,
			onDeployFailure: true,
			onUnhealthy: true,
			healthTimeoutSeconds: 120,
			pauseAutoSync: true,
		});
		expect(update.status).toBe(409);
		expect(update.body.error).toContain("rollback.enabled");
	});

	test("change on sync, and an invalid value fails the sync", async ({ page }) => {
		const { sha } = await commit(repo!.name, {
			files: { "compose.yaml": `x-orcacd:\n  delete-old-images: true\n${services}` },
			message: "Turn off the automatic rollback",
		});
		await syncRepository(page);

		// The removed rollback settings go back to their defaults. The agent
		// never connects, so the application isn't synced and the commit is
		// deployed; without a deployment is covered by the hub tests.
		await expect.poll(async () => (await getApplication(page)).commit).toBe(sha);
		expect(await getApplication(page)).toMatchObject({
			rollback: { enabled: false, healthTimeoutSeconds: 300 },
			managedSettings: ["deleteOldImages"],
		});

		await commit(repo!.name, {
			files: { "compose.yaml": `x-orcacd:\n  rollback:\n    health-timeout: 2h\n${services}` },
			message: "Set an invalid health timeout",
		});
		await syncRepository(page);
		await expect
			.poll(async () => (await getApplication(page)).lastSyncError)
			.toContain("x-orcacd.rollback.health-timeout: must be at most 1h");

		await gotoApp(page, "/applications");
		await expect(
			page.getByText("x-orcacd.rollback.health-timeout", { exact: false }),
		).toBeVisible();
	});

	test("assign the notifications of the application", async ({ page }) => {
		const name = uniqueId("Compose Config Notification");
		const notification = await createNotification(page, {
			name,
			type: "webhook",
			config: JSON.stringify({ webhookUrl: hookUrl("compose-config") }),
			composeAssignable: true,
		});
		try {
			await commit(repo!.name, {
				files: { "compose.yaml": `x-orcacd:\n  notifications: ["${name}"]\n${services}` },
				message: "Assign the notification",
			});
			await syncRepository(page);
			await expect
				.poll(async () => (await getNotification(page, name))?.applicationIds)
				.toEqual([appId]);

			const update = await api(page, "PUT", `/notifications/${notification.id}`, {
				enabled: true,
				allApplications: false,
				allAgents: false,
				allRepositories: false,
				events: ["application.deployment.failed"],
				applicationIds: [],
			});
			expect(update.status).toBe(409);

			await gotoApp(page, "/notifications");
			await page
				.locator("[data-slot='card']", { hasText: name })
				.getByRole("button", { name: "Card actions" })
				.click();
			await page.getByRole("menuitem", { name: "Edit" }).click();
			await expect(
				page.getByRole("dialog").getByText("Some applications set their notifications"),
			).toBeVisible();
		} finally {
			await deleteNotifications(page, [notification.id]);
		}
	});
});
