import { mutate } from "swr";
import { API_BASE } from "./api";

let es: EventSource | undefined = undefined;

export function connect(onUnauthorized?: () => void): void {
	if (es) {
		return;
	}

	es = new EventSource(`${API_BASE}/events`);

	es.addEventListener("update", async (e) => {
		const event = JSON.parse(e.data) as { urls: string[] };
		await mutate((key) => typeof key === "string" && event.urls.some((url) => key.startsWith(url)));
	});

	es.addEventListener("unauthorized", () => {
		disconnect();
		onUnauthorized?.();
	});
}

export function disconnect(): void {
	es?.close();
	es = undefined;
}
