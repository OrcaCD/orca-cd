import { createHash } from "node:crypto";

export interface Branch {
	sha: string;
	message: string;
	commits: number;
	files: Map<string, string>;
}

export interface RecordedRequest {
	method: string;
	path: string;
	authorized: boolean;
}

export interface Repo {
	key: string;
	branches: Map<string, Branch>;
	token?: string;
	requests: RecordedRequest[];
	snapshots: Map<string, Map<string, string>>;
}

export type BranchDefinitions = Record<string, { files?: Record<string, string> }>;

const DEFAULT_BRANCHES: BranchDefinitions = { main: { files: { "README.md": "# E2E\n" } } };

const repos = new Map<string, Repo>();

export function getRepo(key: string) {
	return repos.get(key);
}

export function deleteRepo(key: string) {
	repos.delete(key);
}

// Deterministic, so tests can predict the commit hashes
function commitSha(repo: Repo, branch: string, counter: number) {
	return createHash("sha1").update(`${repo.key}:${branch}:${counter}`).digest("hex");
}

/** Creates a repository, replacing any existing one with the same key */
export function createRepo(
	key: string,
	{ branches = DEFAULT_BRANCHES, token }: { branches?: BranchDefinitions; token?: string },
) {
	const repo: Repo = { key, branches: new Map(), token, requests: [], snapshots: new Map() };
	for (const [name, { files = {} }] of Object.entries(branches)) {
		const branch = {
			sha: commitSha(repo, name, 1),
			message: "Initial commit",
			commits: 1,
			files: new Map(Object.entries(files)),
		};
		repo.branches.set(name, branch);
		repo.snapshots.set(branch.sha, new Map(branch.files));
	}
	repos.set(key, repo);
}

/** Applies the file changes as a new commit, a `null` content deletes the file */
export function commitFiles(
	repo: Repo,
	branchName: string,
	files: Record<string, string | null>,
	message?: string,
) {
	const branch = repo.branches.get(branchName);
	if (!branch) {
		return undefined;
	}
	for (const [path, content] of Object.entries(files)) {
		if (content === null) {
			branch.files.delete(path);
		} else {
			branch.files.set(path, content);
		}
	}
	branch.commits++;
	branch.sha = commitSha(repo, branchName, branch.commits);
	branch.message = message ?? `Commit ${branch.commits}`;
	repo.snapshots.set(branch.sha, new Map(branch.files));
	return { sha: branch.sha, message: branch.message };
}

export function treeEntries(files: Map<string, string>) {
	const dirs = new Set<string>();
	for (const path of files.keys()) {
		const parts = path.split("/");
		for (let i = 1; i < parts.length; i++) {
			dirs.add(parts.slice(0, i).join("/"));
		}
	}
	return [
		...[...dirs].map((path) => ({ path, type: "tree" })),
		...[...files.keys()].map((path) => ({ path, type: "blob" })),
	];
}
