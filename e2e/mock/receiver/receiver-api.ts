import type { IncomingMessage, ServerResponse } from "node:http";
import { pathSegments, send } from "../gitea/http.ts";
import { getHook } from "./store.ts";

/** Records every request to /hooks/{name} and answers with the status configured for that hook. */
export async function handleReceiverApi(req: IncomingMessage, res: ServerResponse) {
	const { pathname } = new URL(req.url ?? "/", "http://receiver");
	const [prefix, name] = pathSegments(pathname);
	if (prefix !== "hooks" || !name) {
		return send(res, 404, { error: "not found" });
	}

	const chunks: Buffer[] = [];
	for await (const chunk of req) {
		chunks.push(chunk as Buffer);
	}

	const hook = getHook(name);
	hook.requests.push({
		method: req.method ?? "",
		path: pathname,
		headers: Object.fromEntries(
			Object.entries(req.headers).map(([key, value]) => [key, [value].flat().join(", ")]),
		),
		body: Buffer.concat(chunks).toString("utf8"),
	});
	if (hook.location) {
		res.writeHead(hook.status, { Location: hook.location }).end();
		return;
	}
	return send(res, hook.status, { received: hook.status < 400 });
}
