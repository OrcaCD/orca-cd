import { useEffect, useState } from "react";
import { API_BASE } from "@/lib/api";
import { appendSample, type StatsPoint, type StatsSample } from "@/lib/container-stats";
import { connectEventStream, type StreamStatus } from "@/lib/event-stream";

export type StatsStatus = "idle" | StreamStatus;

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
		const disconnect = connectEventStream<StatsSample>({
			getUrl: () => url,
			eventName: "stats",
			finalReasons: FINAL_REASONS,
			onData: (sample) => {
				setHistory((current) => ({
					key,
					points: appendSample(current.key === key ? current.points : [], sample),
				}));
			},
			onStatus: (next) => setStatus(next),
		});

		return () => {
			disconnect();
			setStatus("connecting");
		};
	}, [applicationId, containerId, enabled, key]);

	return {
		points: history.key === key ? history.points : [],
		status: enabled ? status : "idle",
	};
}
