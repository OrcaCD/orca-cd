import type { IncomingMessage, ServerResponse } from "node:http";
import { pathSegments, readJson, send } from "./http.ts";
import { commitFiles, createRepo, deleteRepo, getRepo } from "./store.ts";

export async function handleControlApi(req: IncomingMessage, res: ServerResponse) {
	const { pathname } = new URL(req.url ?? "/", "http://control");
	if (pathname === "/__control/health") {
		return send(res, 200, { status: "ok" });
	}

	// /__control/repos/{owner}/{repo}[/files|/token|/requests]
	const [prefix, collection, owner, name, action] = pathSegments(pathname);
	if (prefix !== "__control" || collection !== "repos" || !owner || !name) {
		return send(res, 404, { error: "not found" });
	}
	const key = `${owner}/${name}`;

	if (!action && req.method === "PUT") {
		createRepo(key, await readJson(req));
		return send(res, 204);
	}
	if (!action && req.method === "DELETE") {
		deleteRepo(key);
		return send(res, 204);
	}

	const repo = getRepo(key);
	if (!repo) {
		return send(res, 404, { error: "repository does not exist" });
	}

	if (action === "files" && req.method === "PUT") {
		const { branch, files, message } = await readJson(req);
		const commit = commitFiles(repo, branch, files, message);
		return commit ? send(res, 200, commit) : send(res, 404, { error: "branch does not exist" });
	}
	if (action === "token" && req.method === "PUT") {
		repo.token = (await readJson(req)).token ?? undefined;
		return send(res, 204);
	}
	if (action === "requests" && req.method === "GET") {
		return send(res, 200, repo.requests);
	}
	return send(res, 404, { error: "not found" });
}
