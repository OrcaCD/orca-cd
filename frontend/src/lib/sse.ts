import { mutate } from "swr";
import { API_BASE } from "./api";

const PROFILE_URL = `${API_BASE}/auth/profile`;

let es: EventSource | undefined = undefined;

export function connect(onUnauthorized?: () => void): void {
	if (es) {
		return;
	}

	es = new EventSource(`${API_BASE}/events`);

	es.addEventListener("update", async (e) => {
		const event = JSON.parse(e.data) as { urls: string[] };
		// A changed profile can change the permissions, and with them what every list returns
		const all = event.urls.includes(PROFILE_URL);
		await mutate(
			(key) => typeof key === "string" && (all || event.urls.some((url) => key.startsWith(url))),
		);
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
