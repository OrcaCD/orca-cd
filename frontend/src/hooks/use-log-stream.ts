import { useEffect, useRef, useState } from "react";
import { API_BASE } from "@/lib/api";
import {
	lineKey,
	MAX_LOG_LINES,
	repeatedKeys,
	type LogLine,
	type LogsEvent,
} from "@/lib/container-logs";
import { connectEventStream, type StreamStatus } from "@/lib/event-stream";

const INITIAL_TAIL = 1_000;
const FLUSH_MS = 100;
const FINAL_REASONS = new Set(["container_gone", "completed", "not_found"]);

interface LogBuffer {
	key: string;
	lines: LogLine[];
	dropped: number;
	nextId: number;
}

function streamUrl(baseUrl: string, last: LogLine | undefined) {
	const params = new URLSearchParams({ follow: "true" });
	if (last) {
		params.set("since", last.timestamp);
		params.set("tail", String(MAX_LOG_LINES));
	} else {
		params.set("tail", String(INITIAL_TAIL));
	}
	return `${baseUrl}?${params}`;
}

export function useLogStream({
	applicationId,
	containerId,
	runId,
}: {
	applicationId: string;
	containerId: string;
	runId: string;
}) {
	const key = `${applicationId}/${containerId}`;
	const buffer = useRef<LogBuffer>({ key, lines: [], dropped: 0, nextId: 0 });
	const [view, setView] = useState<{ key: string; lines: LogLine[]; dropped: number }>({
		key,
		lines: [],
		dropped: 0,
	});
	const [connection, setConnection] = useState<{
		status: StreamStatus;
		endReason: string | null;
	}>({ status: "connecting", endReason: null });

	useEffect(() => {
		if (buffer.current.key !== key) {
			buffer.current = { key, lines: [], dropped: 0, nextId: 0 };
		}
		const store = buffer.current;
		const baseUrl = `${API_BASE}/applications/${applicationId}/containers/${containerId}/logs/stream`;

		let flushTimer: ReturnType<typeof setTimeout> | undefined;
		let repeated = new Set<string>();

		const flush = () => {
			flushTimer = undefined;
			setView({ key, lines: store.lines, dropped: store.dropped });
		};

		const append = (event: LogsEvent) => {
			const added = repeated.size
				? event.lines.filter((line) => !repeated.delete(lineKey(line)))
				: event.lines;
			store.dropped += event.droppedLines;
			store.lines = [
				...store.lines,
				...added.map((line) => ({ ...line, id: store.nextId++ })),
			].slice(-MAX_LOG_LINES);
			flushTimer ??= setTimeout(flush, FLUSH_MS);
		};

		const disconnect = connectEventStream<LogsEvent>({
			getUrl: () => {
				repeated = repeatedKeys(store.lines);
				return streamUrl(baseUrl, store.lines.at(-1));
			},
			eventName: "log",
			finalReasons: FINAL_REASONS,
			onData: append,
			onStatus: (status, endReason) => setConnection({ status, endReason }),
		});

		return () => {
			clearTimeout(flushTimer);
			disconnect();
			setConnection({ status: "connecting", endReason: null });
		};
	}, [applicationId, containerId, key, runId]);

	return {
		lines: view.key === key ? view.lines : [],
		dropped: view.key === key ? view.dropped : 0,
		status: connection.status,
		endReason: connection.endReason,
	};
}
