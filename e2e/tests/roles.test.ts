import { test, expect, type Page } from "@playwright/test";
import { ADMIN_ROLE_ID, USER_ROLE_ID } from "./const.ts";
import { api, seedRepository, type ApiRepository } from "./helpers/api.ts";
import { deleteRepo } from "./helpers/gitea-mock.ts";
import { gotoApp } from "./helpers/navigation.ts";
import { uniqueId } from "./helpers/oidc.ts";
import { cleanup, createActiveUser, createUser, expectToast } from "./helpers/users.ts";

interface ApiRole {
	id: string;
	name: string;
	builtIn: "admin" | "user" | null;
	permissions: { resource: string; action: string; resourceId: string | null }[];
	members: { id: string; sources: string[] }[];
	oidcGroups: string[];
}

const createdRoleIds: string[] = [];
const createdRepos: ApiRepository[] = [];

test.afterEach(async ({ page }) => {
	await cleanup(page);
	for (const id of createdRoleIds.splice(0)) {
		await api(page, "DELETE", `/admin/roles/${id}`);
	}
	for (const repo of createdRepos.splice(0)) {
		await api(page, "DELETE", `/repositories/${repo.id}`);
		await deleteRepo(repo.name);
	}
});

async function createRole(
	admin: Page,
	body: Partial<Omit<ApiRole, "id">> & { userIds?: string[] },
) {
	const { status, body: role } = await api(admin, "POST", "/admin/roles", {
		name: uniqueId("E2E Role"),
		description: "",
		permissions: [],
		userIds: [],
		oidcGroups: [],
		...body,
	});
	expect(status, JSON.stringify(role)).toBe(201);
	createdRoleIds.push(role.id);
	return role as ApiRole;
}

async function listRoles(admin: Page) {
	const { status, body } = await api(admin, "GET", "/admin/roles");
	expect(status).toBe(200);
	return body as ApiRole[];
}

async function seedRepo(admin: Page, suffix: string) {
	const repo = await seedRepository(admin, `e2e/rbac-${uniqueId(suffix)}`);
	createdRepos.push(repo);
	return repo;
}

function roleNav(page: Page) {
	return page.getByRole("navigation", { name: "Roles" });
}

test.describe("Roles page", () => {
	test("creates a role with permissions and members", async ({ page }) => {
		const member = await createUser(page);
		const name = uniqueId("Deployers");
		await gotoApp(page, "/admin/roles");
		await expect(roleNav(page).getByRole("button", { name: /^Admin/ })).toBeVisible();
		await expect(roleNav(page).getByRole("button", { name: /^User/ })).toBeVisible();

		await roleNav(page).getByRole("button", { name: "New role" }).click();
		await page.getByLabel("Name", { exact: true }).fill(name);
		await page.getByLabel("Description", { exact: true }).fill("Deploys everything");
		const repositories = page.getByRole("region", { name: "Repositories" });
		await repositories.getByRole("checkbox", { name: "Sync: All Repositories" }).check();
		// Sync includes viewing them
		await expect(
			repositories.getByRole("checkbox", { name: "View: All Repositories" }),
		).toBeChecked();
		await expect(
			repositories.getByRole("checkbox", { name: "View: All Repositories" }),
		).toBeDisabled();

		await page.getByRole("tab", { name: /Members/ }).click();
		await page.getByPlaceholder("Add a member...").fill(member.name);
		await page.getByRole("option", { name: new RegExp(member.name) }).click();
		await expect(page.getByRole("list", { name: "Members" })).toContainText(member.email);

		await page.getByRole("button", { name: "Create role" }).click();
		await expectToast(page, "Role created");
		await expect(roleNav(page).getByRole("button", { name: new RegExp(name) })).toBeVisible();

		const role = (await listRoles(page)).find((item) => item.name === name);
		expect(role).toBeDefined();
		createdRoleIds.push(role!.id);
		expect(role).toMatchObject({
			builtIn: null,
			permissions: [{ resource: "repository", action: "sync", resourceId: null }],
			members: [{ id: member.id, sources: ["manual"] }],
		});

		await page.getByRole("button", { name: "Delete role" }).click();
		await page
			.getByRole("dialog", { name: "Delete role?" })
			.getByRole("button", { name: "Proceed" })
			.click();
		await expectToast(page, "Role deleted");
		expect((await listRoles(page)).some((item) => item.id === role!.id)).toBe(false);
	});

	test("asks before discarding unsaved changes", async ({ page }) => {
		const role = await createRole(page, { name: uniqueId("Editors") });
		await gotoApp(page, `/admin/roles?role=${role.id}`);

		await page.getByLabel("Name", { exact: true }).fill(`${role.name} changed`);
		await expect(page.getByText("Unsaved changes")).toBeVisible();
		await roleNav(page).getByRole("button", { name: /^User/ }).click();

		const dialog = page.getByRole("dialog", { name: "Unsaved changes" });
		await dialog.getByRole("button", { name: "Cancel" }).click();
		await expect(page.getByLabel("Name", { exact: true })).toHaveValue(`${role.name} changed`);

		await roleNav(page).getByRole("button", { name: /^User/ }).click();
		await dialog.getByRole("button", { name: "Discard" }).click();
		await expect(page.getByRole("heading", { name: "User", level: 2 })).toBeVisible();
		expect((await listRoles(page)).find((item) => item.id === role.id)?.name).toBe(role.name);
	});

	test("the admin role keeps its permissions and can't be deleted", async ({ page }) => {
		await gotoApp(page, `/admin/roles?role=${ADMIN_ROLE_ID}`);

		await expect(page.getByText("Admins can do everything")).toBeVisible();
		await expect(page.getByRole("button", { name: "Delete role" })).toHaveCount(0);
		await expect(page.getByRole("region", { name: "Applications" })).toHaveCount(0);

		for (const id of [ADMIN_ROLE_ID, USER_ROLE_ID]) {
			const { status } = await api(page, "DELETE", `/admin/roles/${id}`);
			expect(status).toBe(400);
		}
	});

	test("granting an action on all resources removes the rows it covers", async ({ page }) => {
		const repo = await seedRepo(page, "covered");
		const role = await createRole(page, {
			permissions: [{ resource: "repository", action: "sync", resourceId: repo.id }],
		});
		await gotoApp(page, `/admin/roles?role=${role.id}`);

		const repositories = page.getByRole("region", { name: "Repositories" });
		await expect(repositories.getByRole("checkbox", { name: `Sync: ${repo.name}` })).toBeChecked();
		await expect(repositories).toContainText("Limited access, 1 specific");

		await repositories.getByRole("checkbox", { name: "Sync: All Repositories" }).check();
		await expect(repositories.getByRole("checkbox", { name: `Sync: ${repo.name}` })).toHaveCount(0);
		await expect(repositories).toContainText("Limited access");
		await expect(repositories).not.toContainText("1 specific");
	});

	test("role changes are written to the audit log", async ({ page }) => {
		const role = await createRole(page, {});
		const { body } = await api(page, "GET", `/admin/audit-logs?search=${role.id}`);
		expect(body.items).toEqual([
			expect.objectContaining({ eventType: "created", targetType: "role", targetId: role.id }),
		]);
	});
});

test.describe("Permissions", () => {
	test("a user only sees and changes what their roles allow", async ({ page, browser }) => {
		const visible = await seedRepo(page, "visible");
		const hidden = await seedRepo(page, "hidden");
		const role = await createRole(page, {
			permissions: [{ resource: "repository", action: "sync", resourceId: visible.id }],
		});
		const user = await createActiveUser(page, browser, { roleIds: [role.id] });

		const repositories = await api(user.page, "GET", "/repositories");
		expect(repositories.status).toBe(200);
		expect((repositories.body as ApiRepository[]).map((repo) => repo.id)).toEqual([visible.id]);
		expect((await api(user.page, "GET", `/repositories/${hidden.id}`)).status).toBe(403);
		expect((await api(user.page, "POST", `/repositories/${hidden.id}/sync`)).status).toBe(403);
		expect((await api(user.page, "POST", `/repositories/${visible.id}/sync`)).status).toBeLessThan(
			300,
		);
		expect((await api(user.page, "DELETE", `/repositories/${visible.id}`)).status).toBe(403);
		expect((await api(user.page, "GET", "/agents")).body).toEqual([]);
		expect((await api(user.page, "GET", "/admin/roles")).status).toBe(403);

		await gotoApp(user.page, "/repositories");
		await expect(user.page.getByRole("heading", { name: visible.name })).toBeVisible();
		await expect(user.page.getByRole("heading", { name: hidden.name })).toHaveCount(0);
		await expect(user.page.getByRole("button", { name: "Add Repository" })).toHaveCount(0);
		await expect(user.page.getByRole("link", { name: "Agents" })).toHaveCount(0);
	});

	test("role changes apply to signed in users right away", async ({ page, browser }) => {
		const user = await createActiveUser(page, browser);
		expect((await api(user.page, "GET", "/auth/profile")).body.permissions.agent.all).toContain(
			"create",
		);

		const { status } = await api(page, "PUT", `/admin/users/${user.id}`, {
			name: user.name,
			email: user.email,
			roleIds: [],
		});
		expect(status).toBe(200);

		const profile = (await api(user.page, "GET", "/auth/profile")).body;
		expect(profile).toMatchObject({ isAdmin: false, roles: [] });
		expect(profile.permissions.agent.all).toEqual([]);
		expect((await api(user.page, "POST", "/agents", { name: "Not allowed" })).status).toBe(403);
	});

	test("users see their roles on the profile, updated right away", async ({ page, browser }) => {
		const role = await createRole(page, { name: uniqueId("Readers") });
		const user = await createActiveUser(page, browser, { roleIds: [role.id] });
		await gotoApp(user.page, "/settings/profile");

		const roles = user.page.getByText(role.name, { exact: true });
		await expect(roles).toBeVisible();

		const { status } = await api(page, "PUT", `/admin/roles/${role.id}`, {
			name: role.name,
			description: "",
			permissions: [],
			userIds: [],
			oidcGroups: [],
		});
		expect(status).toBe(200);
		await expect(roles).toHaveCount(0);
		await expect(user.page.getByText("No roles", { exact: true })).toBeVisible();
	});
});
