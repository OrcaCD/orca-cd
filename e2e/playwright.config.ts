import { defineConfig, devices } from "@playwright/test";

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

		/* Collect trace when retrying the failed test. See https://playwright.dev/docs/trace-viewer */
		trace: "on-first-retry",
	},
	/* Configure projects for major browsers */
	projects: [
		// Setup project
		{ name: "setup", testMatch: /.*\.setup\.ts/ },
		// Test on Chromium with authenticated state
		{
			name: "chromium",
			use: { ...devices["Desktop Chrome"], storageState: "playwright/.auth/user.json" },
			dependencies: ["setup"],
		},
	],

	globalTeardown: "./global-teardown.ts",

	webServer: {
		command: "docker compose -f docker-compose.e2e.yml up --force-recreate --build",
		url: "http://127.0.0.1:8090/api/v1/health",
		timeout: 3 * 60 * 1000, // 3 minutes
		reuseExistingServer: false,
		stdout: "pipe",
		stderr: "pipe",
	},
});
