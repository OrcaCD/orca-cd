import type { IncomingMessage, ServerResponse } from "node:http";

export function send(res: ServerResponse, status: number, body?: unknown) {
	if (body === undefined) {
		res.writeHead(status).end();
		return;
	}
	res.writeHead(status, { "Content-Type": "application/json" }).end(JSON.stringify(body));
}

export async function readJson(req: IncomingMessage) {
	const chunks: Buffer[] = [];
	for await (const chunk of req) {
		chunks.push(chunk as Buffer);
	}
	const raw = Buffer.concat(chunks).toString("utf8");
	return raw ? (JSON.parse(raw) as Record<string, any>) : {};
}

// Split before decoding because the hub escapes "/" inside file paths as %2F
export function pathSegments(pathname: string) {
	return pathname
		.split("/")
		.filter(Boolean)
		.map((segment) => decodeURIComponent(segment));
}
