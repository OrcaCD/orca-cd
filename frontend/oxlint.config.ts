import { defineConfig } from "oxlint";

export default defineConfig({
	plugins: ["oxc", "typescript", "unicorn", "react", "react-perf", "promise"],
	env: {
		browser: true,
	},
	options: {
		typeAware: true,
		denyWarnings: true,
	},
	rules: {
		"no-alert": "error",
		"no-console": "error",
		"no-empty": "error",
		"no-empty-function": "error",
		"no-proto": "error",
		"no-regex-spaces": "error",
		eqeqeq: "error",
		"no-prototype-builtins": "error",
		"require-await": "error",
		curly: "error",
		"prefer-await-to-then": "error",
		"func-names": ["error", "as-needed"],
		"no-eval": "error",
		"no-var": "error",
		"no-warning-comments": "error",
		"no-duplicate-imports": "error",
	},
	ignorePatterns: ["dist/", "node_modules/", "build/", "**/routeTree.gen.ts", ".tanstack/"],
});
