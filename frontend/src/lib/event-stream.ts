export type StreamStatus = "connecting" | "live" | "ended";

const RETRY_BASE_MS = 1_000;
const RETRY_MAX_MS = 30_000;
const HIDDEN_CLOSE_MS = 30_000;

export function connectEventStream<T>({
	getUrl,
	eventName,
	finalReasons,
	onData,
	onStatus,
}: {
	getUrl: () => string;
	eventName: string;
	finalReasons: ReadonlySet<string>;
	onData: (data: T) => void;
	onStatus: (status: StreamStatus, endReason: string | null) => void;
}): () => void {
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

	const finish = (endReason: string) => {
		finished = true;
		close();
		onStatus("ended", endReason);
	};

	const retry = () => {
		close();
		onStatus("connecting", null);
		retryTimer = setTimeout(open, Math.min(RETRY_BASE_MS * 2 ** attempts, RETRY_MAX_MS));
		attempts++;
	};

	function open() {
		retryTimer = undefined;
		if (source || finished || suspended) {
			return;
		}

		const stream = new EventSource(getUrl());
		source = stream;
		stream.addEventListener("open", () => onStatus("live", null));
		stream.addEventListener(eventName, (event) => {
			attempts = 0;
			onData(JSON.parse((event as MessageEvent<string>).data) as T);
		});
		stream.addEventListener("end", (event) => {
			const { reason } = JSON.parse(event.data) as { reason: string };
			if (finalReasons.has(reason)) {
				finish(reason);
			} else {
				retry();
			}
		});
		stream.addEventListener("unauthorized", () => finish("unauthorized"));
		stream.addEventListener("error", retry);
	}

	const onVisibilityChange = () => {
		if (document.visibilityState === "hidden") {
			hiddenTimer = setTimeout(() => {
				suspended = !finished;
				close();
			}, HIDDEN_CLOSE_MS);
			return;
		}
		clearTimeout(hiddenTimer);
		if (suspended) {
			suspended = false;
			attempts = 0;
			onStatus("connecting", null);
			open();
		}
	};

	open();
	document.addEventListener("visibilitychange", onVisibilityChange);

	return () => {
		document.removeEventListener("visibilitychange", onVisibilityChange);
		clearTimeout(hiddenTimer);
		close();
	};
}
