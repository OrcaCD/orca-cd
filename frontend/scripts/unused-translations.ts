// oxlint-disable no-console
import { readFileSync } from "node:fs";
import { glob } from "node:fs/promises";
import { join } from "node:path";

const messagesFile = "messages/en.json";
const sourceDir = "src";

function flattenKeys(obj: Record<string, any>, prefix: string = ""): string[] {
	return Object.entries(obj).flatMap(([key, value]) => {
		const fullKey = prefix ? `${prefix}.${key}` : key;

		if (value && typeof value === "object" && !Array.isArray(value)) {
			return flattenKeys(value, fullKey);
		}

		return [fullKey];
	});
}

const messages = JSON.parse(readFileSync(messagesFile, "utf8"));
const allKeys = new Set(flattenKeys(messages));

const usedKeys = new Set<string>();

for await (const file of glob("**/*.{js,jsx,ts,tsx,svelte,vue}", {
	cwd: sourceDir,
	exclude: ["**/node_modules/**", "**/dist/**", "**/.svelte-kit/**", "**/paraglide/**"],
})) {
	const content = readFileSync(join(sourceDir, file), "utf8");

	for (const match of content.matchAll(/\bm\.([a-zA-Z_$][\w$]*)\s*\(/g)) {
		usedKeys.add(match[1]);
	}

	for (const match of content.matchAll(/\bm\[['"`]([^'"`]+)['"`]\]\s*\(/g)) {
		usedKeys.add(match[1]);
	}
}

const unused = [...allKeys]
	.filter((key) => !usedKeys.has(key) && !key.startsWith("$schema"))
	.sort();

if (unused.length === 0) {
	console.log("No unused Paraglide messages found.");
} else {
	console.log("Unused Paraglide messages:");
	for (const key of unused) {
		console.log(`- ${key}`);
	}

	process.exitCode = 1;
}
