import { mutate } from "swr";

let es: EventSource | undefined = undefined;

export function connect(onUnauthorized?: () => void): void {
	if (es) {
		return;
	}
	es = new EventSource("/api/v1/events");
	es.addEventListener("update", (e) => {
		const event = JSON.parse(e.data) as { url: string };
		mutate((key) => typeof key === "string" && key.startsWith(event.url));
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
