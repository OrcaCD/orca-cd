import { ApiError, fetcher } from "./api";
import { m } from "./paraglide/messages";

export const MAX_ENV_VALUE_BYTES = 16 * 1024;
export const MAX_ENV_KEYS_PER_FILE = 1000;

const keyPattern = /^[\p{L}\p{N}_.\-[\]]+$/u;
// Control characters except tab, CR and LF
// oxlint-disable-next-line no-control-regex
const forbiddenValuePattern = /[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F-\u009F]/;
const savedRedeployFailedPrefix = "the changes were saved, but redeploying the application failed";

export interface EnvEntry {
	key: string;
	isSecret: boolean;
	// Only present for variables
	value?: string;
	isSet: boolean;
}

export interface EnvFile {
	path: string;
	services: string[];
	revision: string;
	entries: EnvEntry[];
}

export interface EnvListResponse {
	files: EnvFile[];
}

export interface EnvChange {
	key: string;
	delete: boolean;
	value: string;
	isSecret: boolean;
}

export interface EnvFileChanges {
	path: string;
	expectedRevision: string;
	changes: EnvChange[];
}

export type EnvDraft = { delete: true } | { delete: false; value: string; isSecret: boolean };

// path -> key -> draft
export type EnvDrafts = Record<string, Record<string, EnvDraft>>;

export interface EnvRow {
	key: string;
	isSecret: boolean;
	// The variable value, or the pending value of a secret. Empty for untouched secrets.
	value: string;
	isNew: boolean;
	deleted: boolean;
	draft?: EnvDraft;
	// Only checked for variables that are not deleted
	valueError: EnvValueError | null;
}

function createEnvRow(row: Omit<EnvRow, "valueError">): EnvRow {
	return { ...row, valueError: row.deleted || row.isSecret ? null : validateEnvValue(row.value) };
}

export function buildEnvRows(file: EnvFile, drafts: Record<string, EnvDraft> = {}): EnvRow[] {
	const existing = new Set(file.entries.map((entry) => entry.key));

	const rows = file.entries.map((entry) => {
		const draft = drafts[entry.key];
		const edit = draft && !draft.delete ? draft : undefined;
		return createEnvRow({
			key: entry.key,
			isSecret: edit?.isSecret ?? entry.isSecret,
			value: edit?.value ?? entry.value ?? "",
			isNew: false,
			deleted: draft?.delete ?? false,
			draft,
		});
	});

	for (const [key, draft] of Object.entries(drafts)) {
		if (!existing.has(key) && !draft.delete) {
			rows.push(
				createEnvRow({
					key,
					isSecret: draft.isSecret,
					value: draft.value,
					isNew: true,
					deleted: false,
					draft,
				}),
			);
		}
	}

	return rows;
}

export function setEnvDraft(
	drafts: EnvDrafts,
	path: string,
	key: string,
	entry: EnvEntry | undefined,
	draft: EnvDraft | null,
): EnvDrafts {
	const isUnchanged =
		draft !== null &&
		!draft.delete &&
		entry !== undefined &&
		!entry.isSecret &&
		!draft.isSecret &&
		draft.value === entry.value;

	const nextFileDrafts = { ...drafts[path] };
	if (draft === null || isUnchanged) {
		delete nextFileDrafts[key];
	} else {
		nextFileDrafts[key] = draft;
	}

	const next = { ...drafts };
	if (Object.keys(nextFileDrafts).length === 0) {
		delete next[path];
	} else {
		next[path] = nextFileDrafts;
	}
	return next;
}

export function countEnvDrafts(drafts: EnvDrafts): number {
	return Object.values(drafts).reduce((sum, fileDrafts) => sum + Object.keys(fileDrafts).length, 0);
}

export function buildEnvUpdate(files: EnvFile[], drafts: EnvDrafts): EnvFileChanges[] {
	const result: EnvFileChanges[] = [];

	for (const file of files) {
		const fileDrafts = drafts[file.path];
		if (!fileDrafts) {
			continue;
		}
		result.push({
			path: file.path,
			expectedRevision: file.revision,
			changes: Object.entries(fileDrafts).map(([key, draft]) =>
				draft.delete
					? { key, delete: true, value: "", isSecret: false }
					: { key, delete: false, value: draft.value, isSecret: draft.isSecret },
			),
		});
	}

	return result;
}

export interface EnvFileExample {
	lines: string[];
	addedIndex: number;
}

const envFileExampleFallback = ["services:", "  app:", "    # ...", "    env_file: .env"];

export function buildEnvFileExample(composeFile: string): EnvFileExample {
	const lines = composeFile.split(/\r?\n/);
	if (lines.at(-1) === "") {
		lines.pop();
	}

	const servicesIndex = lines.findIndex((line) => /^services:\s*(#.*)?$/.test(line));
	if (servicesIndex === -1) {
		return { lines: envFileExampleFallback, addedIndex: 3 };
	}

	for (let i = servicesIndex + 1; i < lines.length; i++) {
		const match = /^( +)(["']?)[\w.-]+\2:\s*(#.*)?$/.exec(lines[i]);
		if (!match) {
			if (/^\S/.test(lines[i]) && !lines[i].startsWith("#")) {
				break;
			}
			continue;
		}

		const serviceIndent = match[1].length;
		const next = lines.slice(i + 1).find((line) => line.trim() !== "" && !/^\s*#/.test(line));
		const nextIndent = next ? next.length - next.trimStart().length : 0;
		const childIndent = " ".repeat(nextIndent > serviceIndent ? nextIndent : serviceIndent + 2);

		// The service ends at the next non-blank line that is not indented deeper than its key.
		// Trailing comments and blank lines at that level belong to what follows.
		let last = i;
		for (let j = i + 1; j < lines.length; j++) {
			if (lines[j].trim() === "") {
				continue;
			}
			if (lines[j].length - lines[j].trimStart().length <= serviceIndent) {
				break;
			}
			last = j;
		}

		return {
			lines: [
				...lines.slice(0, last + 1),
				`${childIndent}env_file: .env`,
				...lines.slice(last + 1),
			],
			addedIndex: last + 1,
		};
	}

	return { lines: envFileExampleFallback, addedIndex: 3 };
}

export function updateApplicationEnv(id: string, files: EnvFileChanges[]): Promise<void> {
	return fetcher(`/applications/${id}/env`, "PUT", { files });
}

export function isEnvConflict(err: unknown): boolean {
	return err instanceof ApiError && err.status === 409;
}

export function isEnvSavedButRedeployFailed(err: unknown): boolean {
	return err instanceof ApiError && err.message.startsWith(savedRedeployFailedPrefix);
}

export type EnvKeyError = "empty" | "invalid";
export type EnvValueError = "tooLong" | "invalidCharacters";

export function validateEnvKey(key: string): EnvKeyError | null {
	if (key === "") {
		return "empty";
	}
	return keyPattern.test(key) ? null : "invalid";
}

export function envKeyErrorMessage(error: EnvKeyError): string {
	return error === "empty" ? m.envErrorKeyEmpty() : m.envErrorKeyInvalid();
}

export function envValueErrorMessage(error: EnvValueError): string {
	return error === "tooLong" ? m.envErrorValueTooLong() : m.envErrorValueInvalid();
}

const encoder = new TextEncoder();

export function validateEnvValue(value: string): EnvValueError | null {
	// A UTF-16 code unit is at most 3 UTF-8 bytes, so short values can skip encoding
	if (
		value.length * 3 > MAX_ENV_VALUE_BYTES &&
		encoder.encode(value).length > MAX_ENV_VALUE_BYTES
	) {
		return "tooLong";
	}
	return forbiddenValuePattern.test(value) ? "invalidCharacters" : null;
}
