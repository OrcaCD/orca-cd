import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}

export function getInitials(name: string) {
	const parts = name.split(" ");
	const initials = parts.slice(0, 3).map((part) => part.charAt(0).toUpperCase());
	return initials.join("");
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
