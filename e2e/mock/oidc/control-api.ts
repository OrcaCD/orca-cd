import type { IncomingMessage, ServerResponse } from "node:http";
import { pathSegments, readJson, send } from "../gitea/http.ts";
import { getAuthRequests, setClient, setTamper, setUser } from "./store.ts";

export async function handleControlApi(req: IncomingMessage, res: ServerResponse) {
	const { pathname } = new URL(req.url ?? "/", "http://control");
	if (pathname === "/__control/health") {
		return send(res, 200, { status: "ok" });
	}

	// /__control/users/{sub}, /__control/clients/{clientId}[/requests|/tamper]
	const [prefix, collection, id, action] = pathSegments(pathname);
	if (prefix !== "__control" || !id) {
		return send(res, 404, { error: "not found" });
	}

	if (collection === "users" && !action && req.method === "PUT") {
		setUser(id, await readJson(req));
		return send(res, 204);
	}
	if (collection === "clients" && !action && req.method === "PUT") {
		const { secret, redirectUris, profileSource } = await readJson(req);
		setClient(id, { secret, redirectUris, profileSource });
		return send(res, 204);
	}
	if (collection === "clients" && action === "tamper" && req.method === "PUT") {
		setTamper(id, await readJson(req));
		return send(res, 204);
	}
	if (collection === "clients" && action === "requests" && req.method === "GET") {
		return send(res, 200, getAuthRequests(id));
	}
	return send(res, 404, { error: "not found" });
}
