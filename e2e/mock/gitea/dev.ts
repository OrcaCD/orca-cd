/**
 * Gitea mock for local development (`just run-gitea-mock` or the gitea-mock profile of docker-compose.dev.yml).
 *
 * Serves the repositories in GITEA_MOCK_REPOS_DIR, laid out as <owner>/<repo>/<branch>/<files>, and turns
 * every change on disk into a new commit. An empty directory is seeded with the repositories in dev-repos/.
 */
import { cpSync, existsSync, readdirSync, readFileSync, watch } from "node:fs";
import { createServer } from "node:https";
import { join, relative, resolve, sep } from "node:path";
import { handleGiteaApi } from "./gitea-api.ts";
import {
	commitFiles,
	createBranch,
	createRepo,
	deleteRepo,
	getRepo,
	listRepos,
	useContentAddressedCommits,
	type Repo,
} from "./store.ts";

const reposDir = resolve(process.env.GITEA_MOCK_REPOS_DIR ?? "data/gitea-mock");
const host = process.env.GITEA_MOCK_HOST ?? "127.0.0.1";
const port = Number(process.env.GITEA_MOCK_PORT ?? 3440);
const publicUrl = process.env.GITEA_MOCK_PUBLIC_URL ?? `https://localhost:${port}`;

const certDir = join(import.meta.dirname, "../certs");
// Editor backups and swap files would otherwise show up as commits
const ignoredFile = /^(\.DS_Store|\.git)$|~$|\.sw[a-p]$/;

function log(message: string) {
	process.stdout.write(`${message}\n`);
}

function subdirs(dir: string) {
	return readdirSync(dir, { withFileTypes: true })
		.filter((entry) => entry.isDirectory() && !ignoredFile.test(entry.name))
		.map((entry) => entry.name);
}

function readFiles(dir: string) {
	const files = new Map<string, string>();
	for (const entry of readdirSync(dir, { withFileTypes: true, recursive: true })) {
		const path = join(entry.parentPath, entry.name);
		const relPath = relative(dir, path).split(sep);
		if (entry.isFile() && !relPath.some((part) => ignoredFile.test(part))) {
			files.set(relPath.join("/"), readFileSync(path, "utf8"));
		}
	}
	return files;
}

function syncBranch(repo: Repo, name: string, files: Map<string, string>) {
	const branch = repo.branches.get(name);
	if (!branch) {
		createBranch(repo, name, Object.fromEntries(files));
		log(`Created branch ${name} in ${repo.key}`);
		return;
	}

	const changes: Record<string, string | null> = {};
	for (const [path, content] of files) {
		if (branch.files.get(path) !== content) {
			changes[path] = content;
		}
	}
	for (const path of branch.files.keys()) {
		if (!files.has(path)) {
			changes[path] = null;
		}
	}

	const paths = Object.keys(changes);
	if (paths.length > 0) {
		const commit = commitFiles(repo, name, changes, `Update ${paths.join(", ")}`);
		log(`${repo.key}@${name}: ${commit?.sha.slice(0, 7)} ${commit?.message}`);
	}
}

/** Brings the in-memory repositories in line with the directory */
function sync() {
	const keys = new Set<string>();
	for (const owner of subdirs(reposDir)) {
		for (const name of subdirs(join(reposDir, owner))) {
			const key = `${owner}/${name}`;
			keys.add(key);
			const repoDir = join(reposDir, owner, name);
			const branches = new Map(
				subdirs(repoDir).map((branch) => [branch, readFiles(join(repoDir, branch))]),
			);

			const repo = getRepo(key);
			if (!repo) {
				createRepo(key, {
					branches: Object.fromEntries(
						[...branches].map(([branch, files]) => [branch, { files: Object.fromEntries(files) }]),
					),
				});
				log(`Created ${publicUrl}/${key} (branches: ${[...branches.keys()].join(", ") || "none"})`);
				continue;
			}

			for (const [branch, files] of branches) {
				syncBranch(repo, branch, files);
			}
			for (const branch of repo.branches.keys()) {
				if (!branches.has(branch)) {
					repo.branches.delete(branch);
					log(`Deleted branch ${branch} in ${key}`);
				}
			}
		}
	}

	for (const { key } of listRepos()) {
		if (!keys.has(key)) {
			deleteRepo(key);
			log(`Deleted ${key}`);
		}
	}
}

function trySync() {
	try {
		sync();
	} catch (err) {
		// Usually a file that vanished mid-read, the next change event catches up
		process.stderr.write(`Sync failed: ${String(err)}\n`);
	}
}

// Docker creates bind-mounted directories empty
if (!existsSync(reposDir) || readdirSync(reposDir).length === 0) {
	cpSync(join(import.meta.dirname, "dev-repos"), reposDir, { recursive: true });
	log(`Seeded ${reposDir}`);
}

useContentAddressedCommits();
sync();

let timer: NodeJS.Timeout | undefined;
watch(reposDir, { recursive: true }, () => {
	clearTimeout(timer);
	timer = setTimeout(trySync, 200);
});

createServer(
	{ key: readFileSync(join(certDir, "mock.key")), cert: readFileSync(join(certDir, "mock.pem")) },
	handleGiteaApi,
).listen(port, host, () => {
	log(`Gitea mock: serving ${reposDir} on ${publicUrl}`);
});

for (const signal of ["SIGINT", "SIGTERM"] as const) {
	process.on(signal, () => process.exit(0));
}
