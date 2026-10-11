import type { IncomingMessage, ServerResponse } from "node:http";
import { pathSegments, send } from "./http.ts";
import { getRepo, treeEntries, type Repo } from "./store.ts";

export function handleGiteaApi(req: IncomingMessage, res: ServerResponse) {
	const url = new URL(req.url ?? "/", "https://mock");
	// /api/v1/repos/{owner}/{repo}/{...rest}
	const [api, version, resource, owner, name, ...rest] = pathSegments(url.pathname);
	if (req.method !== "GET" || api !== "api" || version !== "v1" || resource !== "repos") {
		return send(res, 404, { message: "not found" });
	}
	const repo = getRepo(`${owner}/${name}`);
	if (!repo) {
		return send(res, 404, { message: "repository does not exist" });
	}

	const authorized = !repo.token || req.headers.authorization === `token ${repo.token}`;
	repo.requests.push({ method: req.method, path: `${url.pathname}${url.search}`, authorized });
	if (!authorized) {
		return send(res, 401, { message: "token is required" });
	}

	if (rest.length === 0) {
		return send(res, 200, { full_name: repo.key });
	}
	return handleRepoResource(repo, rest, url, res);
}

function handleRepoResource(repo: Repo, rest: string[], url: URL, res: ServerResponse) {
	const [resource, ...path] = rest;
	const param = (key: string) => url.searchParams.get(key) ?? "";

	switch (resource) {
		case "branches": {
			const page = Math.max(1, Number(param("page") || 1));
			const limit = Math.max(1, Number(param("limit") || 30));
			const names = [...repo.branches.keys()].slice((page - 1) * limit, page * limit);
			return send(
				res,
				200,
				names.map((name) => ({ name })),
			);
		}
		case "git": {
			// git/trees/{branch}
			const branch = repo.branches.get(path[1] ?? "");
			if (path[0] !== "trees" || !branch) {
				return send(res, 404, { message: "branch does not exist" });
			}
			return send(res, 200, { sha: branch.sha, tree: treeEntries(branch.files) });
		}
		case "contents": {
			const content = repo.branches.get(param("ref"))?.files.get(path.join("/"));
			if (content === undefined) {
				return send(res, 404, { message: "file does not exist" });
			}
			return send(res, 200, {
				content: Buffer.from(content).toString("base64"),
				encoding: "base64",
			});
		}
		case "commits": {
			const branch = repo.branches.get(param("sha"));
			if (!branch) {
				return send(res, 404, { message: "branch does not exist" });
			}
			return send(res, 200, [{ sha: branch.sha, commit: { message: branch.message } }]);
		}
		default:
			return send(res, 404, { message: "not found" });
	}
}
