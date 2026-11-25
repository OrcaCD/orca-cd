export const MAX_LOG_LINES = 5_000;

export interface LogLine {
	id: number;
	timestamp: string;
	stream: "stdout" | "stderr";
	text: string;
	truncated: boolean;
}

export type LogEntry = Omit<LogLine, "id">;

export interface LogsEvent {
	lines: LogEntry[];
	droppedLines: number;
}

export type LogLevel = "error" | "warn" | "info" | "debug";

const LEVELS: Record<string, LogLevel> = {
	ERROR: "error",
	ERR: "error",
	FATAL: "error",
	CRIT: "error",
	CRITICAL: "error",
	PANIC: "error",
	WARN: "warn",
	WARNING: "warn",
	INFO: "info",
	DEBUG: "debug",
	TRACE: "debug",
};

const LEVEL_PATTERN = /\b(ERROR|ERR|FATAL|CRITICAL|CRIT|PANIC|WARNING|WARN|INFO|DEBUG|TRACE)\b/i;

// Levels sit at the start of a line, a message that mentions "error" later doesn't count.
const LEVEL_SEARCH_LENGTH = 64;

export function findLogLevel(text: string): { level: LogLevel; start: number; end: number } | null {
	const match = LEVEL_PATTERN.exec(text.slice(0, LEVEL_SEARCH_LENGTH));
	if (!match) {
		return null;
	}

	return {
		level: LEVELS[match[1].toUpperCase()],
		start: match.index,
		end: match.index + match[1].length,
	};
}

// Go writes up to nine fractional digits, which Date can't parse everywhere.
function timestampMs(timestamp: string): number {
	return Date.parse(timestamp.replace(/(\.\d{3})\d+/, "$1"));
}

function pad(value: number, length = 2) {
	return String(value).padStart(length, "0");
}

export function formatLogTime(timestamp: string): string {
	const date = new Date(timestampMs(timestamp));
	if (Number.isNaN(date.getTime())) {
		return timestamp;
	}

	return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${pad(date.getMilliseconds(), 3)}`;
}

export function lineKey(line: LogEntry): string {
	return `${line.timestamp}\n${line.text}`;
}

export function repeatedKeys(lines: LogLine[]): Set<string> {
	const last = lines.at(-1);
	const keys = new Set<string>();
	if (!last) {
		return keys;
	}

	const lastMs = timestampMs(last.timestamp);
	for (let index = lines.length - 1; index >= 0; index--) {
		if (timestampMs(lines[index].timestamp) < lastMs) {
			break;
		}
		keys.add(lineKey(lines[index]));
	}
	return keys;
}

const ESCAPE = String.fromCharCode(27);
const ANSI_SEQUENCE = new RegExp(`${ESCAPE}\\[[0-9;?]*[ -/]*[@-~]`, "g");

export function hasAnsi(text: string): boolean {
	return text.includes(ESCAPE);
}

function stripAnsi(text: string): string {
	return hasAnsi(text) ? text.replace(ANSI_SEQUENCE, "") : text;
}

export function filterLines(lines: LogLine[], filter: string): LogLine[] {
	const needle = filter.trim().toLowerCase();
	if (!needle) {
		return lines;
	}

	return lines.filter((line) => stripAnsi(line.text).toLowerCase().includes(needle));
}
