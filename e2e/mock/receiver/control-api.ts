import type { IncomingMessage, ServerResponse } from "node:http";
import { pathSegments, readJson, send } from "../gitea/http.ts";
import { getHook } from "./store.ts";

export async function handleControlApi(req: IncomingMessage, res: ServerResponse) {
	const { pathname } = new URL(req.url ?? "/", "http://control");
	if (pathname === "/__control/health") {
		return send(res, 200, { status: "ok" });
	}

	// /__control/hooks/{name}[/status]
	const [prefix, collection, name, action] = pathSegments(pathname);
	if (prefix !== "__control" || collection !== "hooks" || !name) {
		return send(res, 404, { error: "not found" });
	}
	const hook = getHook(name);

	if (!action && req.method === "GET") {
		return send(res, 200, hook.requests);
	}
	if (!action && req.method === "DELETE") {
		hook.requests = [];
		return send(res, 204);
	}
	if (action === "status" && req.method === "PUT") {
		const { status, location } = await readJson(req);
		hook.status = status;
		hook.location = location;
		return send(res, 204);
	}
	return send(res, 404, { error: "not found" });
}
