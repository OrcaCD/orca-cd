import type { RefinementCtx } from "zod";
import { getLocale } from "@/lib/paraglide/runtime";

export { cn } from "cn";

export function passwordStrengthRefine(messages: {
	uppercase: string;
	lowercase: string;
	number: string;
	special: string;
}) {
	return (val: string, ctx: RefinementCtx) => {
		if (!/\p{Lu}/u.test(val)) {
			ctx.addIssue({ code: "custom", message: messages.uppercase });
		}
		if (!/\p{Ll}/u.test(val)) {
			ctx.addIssue({ code: "custom", message: messages.lowercase });
		}
		if (!/\p{Nd}/u.test(val)) {
			ctx.addIssue({ code: "custom", message: messages.number });
		}
		if (!/[^\p{Lu}\p{Ll}\p{Nd}]/u.test(val)) {
			ctx.addIssue({ code: "custom", message: messages.special });
		}
	};
}

export function toSearchableText(value: unknown): string {
	if (value === null || value === undefined) {
		return "";
	}

	if (value instanceof Date) {
		return value.toISOString().toLowerCase();
	}

	if (Array.isArray(value)) {
		return value.map(toSearchableText).join(" ");
	}

	switch (typeof value) {
		case "string":
			return value.toLowerCase();
		case "number":
		case "boolean":
		case "bigint":
			return String(value).toLowerCase();
		case "object":
			return Object.values(value as Record<string, unknown>)
				.map(toSearchableText)
				.join(" ");
		default:
			return "";
	}
}

const relativeTimeUnits: [Intl.RelativeTimeFormatUnit, number][] = [
	["year", 365 * 24 * 60 * 60],
	["month", 30 * 24 * 60 * 60],
	["day", 24 * 60 * 60],
	["hour", 60 * 60],
	["minute", 60],
];

export function formatRelativeTime(value: string | Date, now = Date.now()): string {
	const seconds = Math.round((new Date(value).getTime() - now) / 1000);
	const formatter = new Intl.RelativeTimeFormat(getLocale(), { numeric: "auto" });
	for (const [unit, unitSeconds] of relativeTimeUnits) {
		if (Math.abs(seconds) >= unitSeconds) {
			return formatter.format(Math.round(seconds / unitSeconds), unit);
		}
	}
	return formatter.format(seconds, "second");
}

/** Formats an interval in its largest whole unit, e.g. 300 → "5m", 7200 → "2h", 90 → "90s". */
export function formatInterval(seconds: number): string {
	if (seconds > 0 && seconds % 3600 === 0) {
		return `${seconds / 3600}h`;
	}
	if (seconds > 0 && seconds % 60 === 0) {
		return `${seconds / 60}m`;
	}
	return `${seconds}s`;
}

export function formatDuration(ms: number): string {
	const seconds = Math.max(0, Math.round(ms / 1000));
	if (seconds < 60) {
		return `${seconds}s`;
	}
	const minutes = Math.floor(seconds / 60);
	if (minutes < 60) {
		return `${minutes}m ${seconds % 60}s`;
	}
	return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

export const isApple = /Mac|iPod|iPhone|iPad/.test(navigator.platform);

export function groupBy<T, K>(items: T[], getKey: (item: T) => K): Map<K, T[]> {
	const groups = new Map<K, T[]>();
	for (const item of items) {
		const key = getKey(item);
		const group = groups.get(key);
		if (group) {
			group.push(item);
		} else {
			groups.set(key, [item]);
		}
	}
	return groups;
}
