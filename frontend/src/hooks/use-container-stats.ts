import { useEffect, useState } from "react";
import { API_BASE } from "@/lib/api";
import { appendSample, type StatsPoint, type StatsSample } from "@/lib/container-stats";

export type StatsStatus = "idle" | "connecting" | "live" | "ended";

const RETRY_BASE_MS = 1_000;
const RETRY_MAX_MS = 30_000;
const HIDDEN_CLOSE_MS = 30_000;
const FINAL_REASONS = new Set(["container_gone", "not_found"]);

export function useContainerStats({
	applicationId,
	containerId,
	enabled,
}: {
	applicationId: string;
	containerId: string;
	enabled: boolean;
}) {
	const key = `${applicationId}/${containerId}`;
	const [history, setHistory] = useState<{ key: string; points: StatsPoint[] }>({
		key,
		points: [],
	});
	const [status, setStatus] = useState<StatsStatus>("connecting");

	useEffect(() => {
		if (!enabled) {
			return;
		}

		const url = `${API_BASE}/applications/${applicationId}/containers/${containerId}/stats/stream`;
		let source: EventSource | undefined;
		let retryTimer: ReturnType<typeof setTimeout> | undefined;
		let hiddenTimer: ReturnType<typeof setTimeout> | undefined;
		let attempts = 0;
		let finished = false;
		let suspended = false;

		const close = () => {
			source?.close();
			source = undefined;
			clearTimeout(retryTimer);
			retryTimer = undefined;
		};

		const finish = () => {
			finished = true;
			close();
			setStatus("ended");
		};

		const retry = () => {
			close();
			setStatus("connecting");
			retryTimer = setTimeout(open, Math.min(RETRY_BASE_MS * 2 ** attempts, RETRY_MAX_MS));
			attempts++;
		};

		function open() {
			retryTimer = undefined;
			if (source || finished || suspended) {
				return;
			}

			const stream = new EventSource(url);
			source = stream;
			stream.addEventListener("open", () => setStatus("live"));
			stream.addEventListener("stats", (event) => {
				attempts = 0;
				const sample = JSON.parse(event.data) as StatsSample;
				setHistory((current) => ({
					key,
					points: appendSample(current.key === key ? current.points : [], sample),
				}));
			});
			stream.addEventListener("end", (event) => {
				const { reason } = JSON.parse(event.data) as { reason: string };
				if (FINAL_REASONS.has(reason)) {
					finish();
				} else {
					retry();
				}
			});
			stream.addEventListener("unauthorized", finish);
			stream.addEventListener("error", retry);
		}

		const onVisibilityChange = () => {
			if (document.visibilityState === "hidden") {
				hiddenTimer = setTimeout(() => {
					suspended = true;
					close();
				}, HIDDEN_CLOSE_MS);
				return;
			}
			clearTimeout(hiddenTimer);
			if (suspended) {
				suspended = false;
				attempts = 0;
				setStatus("connecting");
				open();
			}
		};

		open();
		document.addEventListener("visibilitychange", onVisibilityChange);

		return () => {
			document.removeEventListener("visibilitychange", onVisibilityChange);
			clearTimeout(hiddenTimer);
			close();
			setStatus("connecting");
		};
	}, [applicationId, containerId, enabled, key]);

	return {
		points: history.key === key ? history.points : [],
		status: enabled ? status : "idle",
	};
}
