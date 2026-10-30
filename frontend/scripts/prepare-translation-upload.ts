// oxlint-disable no-console
// Reduces every non-default locale file to the messages that changed since the given git
// revision, so uploading them to Crowdin cannot overwrite translations made in Crowdin.
import { execFileSync } from "node:child_process";
import { appendFileSync, readFileSync, writeFileSync } from "node:fs";
import { glob } from "node:fs/promises";
import { join, resolve } from "node:path";

const baseRevision = process.argv[2];

if (!baseRevision) {
	throw new Error("Usage: node scripts/prepare-translation-upload.ts <base-revision>");
}

const root = resolve(import.meta.dirname, "..");
const messagesDir = join(root, "messages");
const defaultLocaleFile = "en.json";

type Messages = Record<string, unknown>;

function readPreviousMessages(file: string): Messages {
	try {
		const content = execFileSync("git", ["show", `${baseRevision}:./messages/${file}`], {
			cwd: root,
			encoding: "utf8",
			stdio: ["ignore", "pipe", "ignore"],
		});
		return JSON.parse(content) as Messages;
	} catch {
		// The locale did not exist at the base revision, so all of its messages are new.
		return {};
	}
}

let hasChanges = false;

for await (const file of glob("*.json", { cwd: messagesDir })) {
	if (file === defaultLocaleFile) {
		continue;
	}

	const path = join(messagesDir, file);
	const current = JSON.parse(readFileSync(path, "utf8")) as Messages;
	const previous = readPreviousMessages(file);

	const changed = Object.fromEntries(
		Object.entries(current).filter(
			([key, value]) => !key.startsWith("$schema") && previous[key] !== value,
		),
	);
	const changedKeys = Object.keys(changed);

	writeFileSync(path, `${JSON.stringify({ $schema: current.$schema, ...changed }, null, "\t")}\n`);

	if (changedKeys.length > 0) {
		hasChanges = true;
		console.log(`${file}: ${changedKeys.length} changed message(s)`);
		for (const key of changedKeys) {
			console.log(`- ${key}`);
		}
	} else {
		console.log(`${file}: no changes`);
	}
}

if (process.env.GITHUB_OUTPUT) {
	appendFileSync(process.env.GITHUB_OUTPUT, `has_changes=${hasChanges}\n`);
}
