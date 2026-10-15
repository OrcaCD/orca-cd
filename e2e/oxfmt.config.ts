import { defineConfig } from "oxfmt";

export default defineConfig({
	ignorePatterns: ["playwright/", "playwright-report/", "test-results/"],
	useTabs: true,
});
