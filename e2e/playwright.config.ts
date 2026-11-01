import { defineConfig, devices } from "@playwright/test";
import {
	GITEA_MOCK_CONTROL_URL,
	OIDC_MOCK_CONTROL_URL,
	RECEIVER_MOCK_CONTROL_URL,
} from "./tests/const.ts";

const skipBuild = ["1", "true"].includes(process.env.E2E_SKIP_BUILD ?? "");

const authenticated = {
	...devices["Desktop Chrome"],
	storageState: "playwright/.auth/user.json",
};

// These files share the single agent container and restart the hub, so they must not overlap
// with other tests and run one after another after everything else
const sharedInfrastructure = /(agent|applications|images|users)\.test\.ts$/;
// CI runs them in a separate job with its own hub, so they don't have to wait for the other tests
const separateSharedInfrastructure = ["1", "true"].includes(
	process.env.E2E_SEPARATE_SHARED_INFRASTRUCTURE ?? "",
);
const compose = "docker compose -f docker-compose.e2e.yml";
// CI loads cached images before Playwright starts; local runs still build them.
const buildImages = skipBuild ? "" : `${compose} --profile agent build && `;

/**
 * See https://playwright.dev/docs/test-configuration.
 */
export default defineConfig({
	testDir: "./tests",
	/* Run tests in files in parallel */
	fullyParallel: false,
	/* Fail the build on CI if you accidentally left test.only in the source code. */
	forbidOnly: !!process.env.CI,
	/* Retry on CI only */
	retries: process.env.CI ? 2 : 0,
	/* Opt out of parallel tests on CI. */
	workers: process.env.CI ? 1 : undefined,
	/* Reporter to use. See https://playwright.dev/docs/test-reporters */
	reporter: [["html", { open: "never" }]],
	/* Shared settings for all the projects below. See https://playwright.dev/docs/api/class-testoptions. */
	use: {
		baseURL: "http://127.0.0.1:8090",
		launchOptions: { args: ["--host-resolver-rules=MAP host.docker.internal 127.0.0.1"] },

		/* A first failure has no trace otherwise, which makes flaky failures on CI hard to debug. See https://playwright.dev/docs/trace-viewer */
		trace: "retain-on-failure",
	},
	/* Configure projects for major browsers */
	projects: [
		// Setup project
		{ name: "setup", testMatch: /.*\.setup\.ts/ },
		// Test on Chromium with authenticated state
		{
			name: "chromium",
			testIgnore: sharedInfrastructure,
			use: authenticated,
			dependencies: ["setup"],
		},
		{
			name: "agent",
			testMatch: /agent\.test\.ts$/,
			use: authenticated,
			dependencies: [separateSharedInfrastructure ? "setup" : "chromium"],
		},
		{
			name: "applications",
			testMatch: /applications\.test\.ts$/,
			use: authenticated,
			dependencies: ["agent"],
		},
		{
			name: "images",
			testMatch: /images\.test\.ts$/,
			use: authenticated,
			dependencies: ["applications"],
		},
		{
			name: "users",
			testMatch: /users\.test\.ts$/,
			use: authenticated,
			dependencies: ["images"],
		},
	],

	globalTeardown: "./global-teardown.ts",

	webServer: [
		{
			command: `${buildImages}${compose} up --force-recreate --no-build hub`,
			url: "http://127.0.0.1:8090/api/v1/health",
			timeout: 6 * 60 * 1000, // 6 minutes
			reuseExistingServer: false,
			stdout: "pipe",
			stderr: "pipe",
		},
		{
			command: "node mock/gitea/app.ts",
			url: `${GITEA_MOCK_CONTROL_URL}/__control/health`,
			reuseExistingServer: false,
			stdout: "pipe",
			stderr: "pipe",
		},
		{
			command: "node mock/receiver/app.ts",
			url: `${RECEIVER_MOCK_CONTROL_URL}/__control/health`,
			reuseExistingServer: false,
			stdout: "pipe",
			stderr: "pipe",
		},
		{
			command: "node mock/oidc/app.ts",
			url: `${OIDC_MOCK_CONTROL_URL}/__control/health`,
			reuseExistingServer: false,
			stdout: "pipe",
			stderr: "pipe",
		},
	],
});
