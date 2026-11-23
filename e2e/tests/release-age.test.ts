import { test, expect, type Page } from "@playwright/test";
import { api, seedRepository, type ApiRepository } from "./helpers/api.ts";
import { deleteRepo } from "./helpers/gitea-mock.ts";
import { gotoApp } from "./helpers/navigation.ts";
import { uniqueId } from "./helpers/oidc.ts";
import { expectToast } from "./helpers/users.ts";

const day = 24 * 60 * 60;

let repo: ApiRepository | undefined;
let agentId = "";
let appId = "";

async function seedApplication(page: Page) {
	repo = await seedRepository(
		page,
		`e2e/release-age-${uniqueId("repo")}`,
		{},
		{
			branches: {
				main: { files: { "compose.yaml": "services:\n  web:\n    image: busybox:1.37\n" } },
			},
		},
	);
	// The agent never connects; the application only needs it to exist.
	const agent = await api(page, "POST", "/agents", { name: uniqueId("Release Age Agent") });
	expect(agent.status).toBe(201);
	agentId = agent.body.id as string;
	const created = await api(page, "POST", "/applications", {
		name: uniqueId("Release Age App"),
		repositoryId: repo.id,
		agentId,
		branch: "main",
		path: "compose.yaml",
	});
	expect(created.status, JSON.stringify(created.body)).toBe(201);
	appId = created.body.id as string;
}

function releaseAgeCard(page: Page) {
	return page.locator('[data-slot="card"]').filter({ hasText: "Minimum Release Age" });
}

test.describe("Release age", () => {
	test.beforeEach(async ({ page }) => {
		await seedApplication(page);
	});

	test.afterEach(async ({ page }) => {
		// The global minimum applies to every application.
		await api(page, "PUT", "/admin/settings/release-age", {
			minimumSeconds: 0,
			allowedRegistries: [],
		});
		await api(page, "DELETE", `/applications/${appId}`);
		await api(page, "DELETE", `/agents/${agentId}`);
		if (repo) {
			await api(page, "DELETE", `/repositories/${repo.id}`);
			await deleteRepo(repo.name);
		}
	});

	test("the upcoming updates show the minimum release age before the first check", async ({
		page,
	}) => {
		await gotoApp(page, `/applications/${appId}/image-updates`);
		await expect(page.getByText("Minimum release age: Off")).toBeVisible();
		await expect(page.getByText("No image check yet.", { exact: false })).toBeVisible();
	});

	test("applications can't go below the global minimum", async ({ page }) => {
		await gotoApp(page, "/admin/release-age");
		await page.getByLabel("Minimum age").fill("2");
		await page.getByRole("button", { name: "Save Changes" }).click();
		await expectToast(page, "Release age settings updated");
		expect((await api(page, "GET", "/settings/release-age")).body).toMatchObject({
			minimumSeconds: 2 * day,
		});

		await gotoApp(page, `/applications/${appId}/settings/image-polling`);
		const card = releaseAgeCard(page);
		await expect(card.getByText("Currently 2 days.", { exact: false })).toBeVisible();

		// Turning off the global default starts from the global minimum, not from "Off".
		await card.getByRole("switch", { name: "Use the global default" }).click();
		const age = card.getByLabel("Minimum age");
		await expect(age).toHaveValue("2");

		await age.fill("1");
		await card.getByRole("button", { name: "Save Changes" }).click();
		await expect(
			card.getByText("The minimum age must be at least the global minimum of 2 days"),
		).toBeVisible();

		expect(
			(
				await api(page, "PUT", `/applications/${appId}/release-age/settings`, {
					minimumSeconds: day,
					allowedRegistries: [],
				})
			).status,
		).toBe(400);

		await age.fill("3");
		await card.getByRole("button", { name: "Save Changes" }).click();
		await expectToast(page, "Application updated");
		expect((await api(page, "GET", `/applications/${appId}`)).body).toMatchObject({
			releaseAge: { minimumSeconds: 3 * day },
		});
	});

	test("the age input keeps the unit of the saved age", async ({ page }) => {
		const updated = await api(page, "PUT", `/applications/${appId}/release-age/settings`, {
			minimumSeconds: 36 * 60 * 60,
			allowedRegistries: [],
		});
		expect(updated.status).toBe(200);

		await gotoApp(page, `/applications/${appId}/settings/image-polling`);
		const card = releaseAgeCard(page);
		const age = card.getByLabel("Minimum age");
		await expect(age).toHaveValue("36");

		// A fraction of a day is shown as it is saved, not rounded.
		await card.getByLabel("Unit").click();
		await page.getByRole("option", { name: "days", exact: true }).click();
		await age.fill("0.5");
		await expect(age).toHaveValue("0.5");

		// Cancel goes back to the saved age in hours.
		await card.getByRole("button", { name: "Cancel" }).click();
		await expect(age).toHaveValue("36");
	});
});
