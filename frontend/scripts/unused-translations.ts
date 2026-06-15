// oxlint-disable no-console
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

const messagesFile = "messages/en.json";
const sourceDir = "src";

function walk(dir: string): string[] {
	return readdirSync(dir).flatMap((entry) => {
		const path = join(dir, entry);
		const stat = statSync(path);

		if (stat.isDirectory()) {
			if (
				entry === "node_modules" ||
				entry === "dist" ||
				entry === ".svelte-kit" ||
				entry === "paraglide"
			) {
				return [];
			}

			return walk(path);
		}

		if (!/\.(js|jsx|ts|tsx|svelte|vue)$/.test(entry)) {
			return [];
		}

		return [path];
	});
}

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

const usedKeys = new Set();

for (const file of walk(sourceDir)) {
	const content = readFileSync(file, "utf8");

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
