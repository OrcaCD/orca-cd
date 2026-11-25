import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useLogStream } from "@/hooks/use-log-stream";
import {
	filterLines,
	findLogLevel,
	formatLogTime,
	hasAnsi,
	type LogLevel,
	type LogLine,
} from "@/lib/container-logs";
import type { StreamStatus } from "@/lib/event-stream";
import { loadHighlighter } from "@/lib/highlighter";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";

const ESTIMATED_ROW_HEIGHT = 20;
const BOTTOM_THRESHOLD = 24;

const LEVEL_CLASSES: Record<LogLevel, string> = {
	error: "font-semibold text-red-600 dark:text-red-400",
	warn: "font-semibold text-amber-600 dark:text-amber-400",
	info: "text-sky-600 dark:text-sky-400",
	debug: "text-muted-foreground",
};

type AnsiHighlighter = NonNullable<Awaited<ReturnType<typeof loadHighlighter>>>;

function useAnsiHighlighter(needed: boolean) {
	const [highlighter, setHighlighter] = useState<AnsiHighlighter | null>(null);

	useEffect(() => {
		if (!needed) {
			return;
		}

		let cancelled = false;
		const load = async () => {
			try {
				const loaded = await loadHighlighter("ansi");
				if (!cancelled) {
					setHighlighter(loaded);
				}
			} catch {
				// The escape sequences stay in the text if the highlighter fails to load.
			}
		};
		void load();

		return () => {
			cancelled = true;
		};
	}, [needed]);

	return highlighter;
}

function LogText({ line, ansi }: { line: LogLine; ansi: AnsiHighlighter | null }) {
	if (hasAnsi(line.text)) {
		return ansi ? ansi.highlight(line.text).lines[0] : line.text;
	}

	const found = findLogLevel(line.text);
	if (!found) {
		return line.text;
	}

	return (
		<>
			{line.text.slice(0, found.start)}
			<span className={LEVEL_CLASSES[found.level]}>{line.text.slice(found.start, found.end)}</span>
			{line.text.slice(found.end)}
		</>
	);
}

function LogRow({ line, ansi }: { line: LogLine; ansi: AnsiHighlighter | null }) {
	return (
		<div
			className={cn(
				"flex gap-3 px-3 py-px leading-5",
				line.stream === "stderr" && "bg-destructive/5",
			)}
		>
			<time
				dateTime={line.timestamp}
				title={line.timestamp}
				className="shrink-0 text-muted-foreground tabular-nums select-none"
			>
				{formatLogTime(line.timestamp)}
			</time>
			<span className="min-w-0 flex-1 break-all whitespace-pre-wrap">
				<LogText line={line} ansi={ansi} />
				{line.truncated ? (
					<span className="ms-1 text-muted-foreground">{m.monitoringLogsTruncated()}</span>
				) : null}
			</span>
		</div>
	);
}

function statusLabel(status: StreamStatus, endReason: string | null): string {
	if (status === "live") {
		return m.monitoringLogsLive();
	}
	if (status === "connecting") {
		return m.monitoringLogsConnecting();
	}

	switch (endReason) {
		case "container_gone":
			return m.monitoringLogsEndedStopped();
		case "unauthorized":
			return m.monitoringLogsEndedUnauthorized();
		case "not_found":
			return m.monitoringLogsEndedNotFound();
		default:
			return m.monitoringLogsEnded();
	}
}

export function LogsPanel({
	applicationId,
	containerId,
	runId,
	filter,
	follow,
	onFollowChange,
}: {
	applicationId: string;
	containerId: string;
	runId: string;
	filter: string;
	follow: boolean;
	onFollowChange: (follow: boolean) => void;
}) {
	const { lines, dropped, status, endReason } = useLogStream({ applicationId, containerId, runId });
	const visibleLines = useMemo(() => filterLines(lines, filter), [lines, filter]);
	const filtering = filter.trim() !== "";

	const scrollRef = useRef<HTMLDivElement>(null);
	const lastScroll = useRef({ top: 0, height: 0 });
	// The virtualizer re-renders the component itself, nothing below it is memoized.
	// oxlint-disable-next-line react/incompatible-library
	const virtualizer = useVirtualizer({
		count: visibleLines.length,
		getScrollElement: () => scrollRef.current,
		estimateSize: () => ESTIMATED_ROW_HEIGHT,
		getItemKey: (index) => visibleLines[index].id,
		overscan: 20,
	});
	const items = virtualizer.getVirtualItems();
	const totalSize = virtualizer.getTotalSize();
	const ansi = useAnsiHighlighter(items.some((item) => hasAnsi(visibleLines[item.index].text)));

	useLayoutEffect(() => {
		const element = scrollRef.current;
		if (follow && element) {
			element.scrollTop = element.scrollHeight;
		}
	}, [follow, visibleLines.length, totalSize]);

	const handleScroll = () => {
		const element = scrollRef.current;
		if (!element) {
			return;
		}

		const previous = lastScroll.current;
		lastScroll.current = { top: element.scrollTop, height: element.scrollHeight };
		const atBottom =
			element.scrollHeight - element.scrollTop - element.clientHeight <= BOTTOM_THRESHOLD;
		// A smaller scroll height moves the position too, which isn't the user scrolling up.
		if (
			follow &&
			!atBottom &&
			element.scrollTop < previous.top &&
			element.scrollHeight >= previous.height
		) {
			onFollowChange(false);
		}
	};

	const emptyMessage =
		lines.length > 0
			? m.monitoringLogsNoMatches()
			: status === "ended"
				? m.monitoringLogsEmpty()
				: m.monitoringLogsWaiting();

	return (
		<div className="flex flex-col gap-2">
			<div
				ref={scrollRef}
				onScroll={handleScroll}
				role="log"
				aria-label={m.monitoringLogs()}
				tabIndex={0}
				className="h-[32rem] scrollbar-thin overflow-auto rounded-lg border bg-card font-mono text-xs outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
			>
				{visibleLines.length === 0 ? (
					<p className="p-4 font-sans text-sm text-muted-foreground">{emptyMessage}</p>
				) : (
					<div className="relative w-full" style={{ height: totalSize }}>
						{items.map((item) => (
							<div
								key={item.key}
								ref={virtualizer.measureElement}
								data-index={item.index}
								className="absolute inset-x-0 top-0"
								style={{ transform: `translateY(${item.start}px)` }}
							>
								<LogRow line={visibleLines[item.index]} ansi={ansi} />
							</div>
						))}
					</div>
				)}
			</div>
			<div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-xs text-muted-foreground">
				<div className="flex flex-wrap items-center gap-x-4 gap-y-1">
					<span className="flex items-center gap-1.5">
						<span
							aria-hidden="true"
							className={cn(
								"size-2 rounded-full",
								status === "live" ? "bg-emerald-500" : "bg-muted-foreground/50",
							)}
						/>
						{statusLabel(status, endReason)}
					</span>
					{filtering ? <span>{m.monitoringLogsFilterHint()}</span> : null}
				</div>
				{dropped > 0 ? (
					<span className="text-amber-600 dark:text-amber-400">
						{m.monitoringLogsDropped({ count: dropped })}
					</span>
				) : null}
				<span>
					{filtering
						? m.monitoringLogsFiltered({ shown: visibleLines.length, total: lines.length })
						: m.monitoringLogsLoaded({ count: lines.length })}
				</span>
			</div>
		</div>
	);
}
