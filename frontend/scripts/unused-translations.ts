// oxlint-disable no-console
import { readFileSync } from "node:fs";
import { glob } from "node:fs/promises";
import { join } from "node:path";

const messagesDir = "messages";
const sourceDir = "src";

function flattenKeys(obj: Record<string, unknown>, prefix: string = ""): string[] {
	return Object.entries(obj).flatMap(([key, value]) => {
		const fullKey = prefix ? `${prefix}.${key}` : key;

		if (value && typeof value === "object" && !Array.isArray(value)) {
			return flattenKeys(value as Record<string, unknown>, fullKey);
		}

		return [fullKey];
	});
}

const messageFiles: string[] = [];

for await (const file of glob("*.json", {
	cwd: messagesDir,
})) {
	messageFiles.push(file);
}

messageFiles.sort();

const messageKeysByFile = new Map<string, Set<string>>();

for (const file of messageFiles) {
	const messages = JSON.parse(readFileSync(join(messagesDir, file), "utf8")) as Record<
		string,
		unknown
	>;

	messageKeysByFile.set(file, new Set(flattenKeys(messages)));
}

const usedKeys = new Set<string>();

for await (const file of glob("**/*.{js,jsx,ts,tsx}", {
	cwd: sourceDir,
	exclude: ["**/node_modules/**", "**/dist/**", "**/paraglide/**", "**/.tanstack/**"],
})) {
	const content = readFileSync(join(sourceDir, file), "utf8");

	for (const match of content.matchAll(/\bm\.([a-zA-Z_$][\w$]*)\s*\(/g)) {
		usedKeys.add(match[1]);
	}

	for (const match of content.matchAll(/\bm\[['"`]([^'"`]+)['"`]\]\s*\(/g)) {
		usedKeys.add(match[1]);
	}
}

const unusedByFile = new Map<string, string[]>();

for (const [file, keys] of messageKeysByFile) {
	const unused = [...keys].filter((key) => !usedKeys.has(key) && !key.startsWith("$schema")).sort();

	if (unused.length > 0) {
		unusedByFile.set(file, unused);
	}
}

if (unusedByFile.size === 0) {
	console.log("No unused Paraglide messages found.");
} else {
	console.log("Unused Paraglide messages:");
	for (const [file, unused] of unusedByFile) {
		console.log(`${file}:`);
		for (const key of unused) {
			console.log(`- ${key}`);
		}
	}

	process.exitCode = 1;
}
