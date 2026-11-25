import { randomUUID } from "node:crypto";
import type { Page } from "@playwright/test";
import { APP_URL } from "../const.ts";

export interface StreamEvent {
	name: string;
	data: any;
}

declare global {
	interface Window {
		__eventStreams?: Record<string, { events: StreamEvent[]; source: EventSource }>;
	}
}

export async function openEventStream(page: Page, path: string) {
	if (!page.url().startsWith(APP_URL)) {
		await page.goto(`${APP_URL}/api/v1/health`);
	}
	const id = randomUUID();
	await page.evaluate(
		({ id, url }) => {
			const events: StreamEvent[] = [];
			const source = new EventSource(url);
			for (const name of ["log", "stats", "end", "unauthorized"]) {
				source.addEventListener(name, (event) => {
					events.push({ name, data: JSON.parse((event as MessageEvent).data) });
					// The server closes the response after these, which would make the
					// browser reconnect and open another stream
					if (name === "end" || name === "unauthorized") {
						source.close();
					}
				});
			}
			window.__eventStreams = { ...window.__eventStreams, [id]: { events, source } };
		},
		{ id, url: `/api/v1${path}` },
	);

	return {
		events: () =>
			page.evaluate((id) => window.__eventStreams?.[id]?.events ?? [], id) as Promise<
				StreamEvent[]
			>,
		/** 0 = connecting, 1 = open, 2 = closed */
		readyState: () =>
			page.evaluate((id) => window.__eventStreams?.[id]?.source.readyState ?? 2, id),
		close: () => page.evaluate((id) => window.__eventStreams?.[id]?.source.close(), id),
	};
}

export type EventStreamHandle = Awaited<ReturnType<typeof openEventStream>>;

export async function loggedLines(stream: EventStreamHandle) {
	const events = await stream.events();
	return events
		.filter((event) => event.name === "log")
		.flatMap((event) => event.data.lines as { timestamp: string; stream: string; text: string }[]);
}
