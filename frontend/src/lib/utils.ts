import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";
import type { RefinementCtx } from "zod";

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}

export function getInitials(name: string) {
	const parts = name.split(" ");
	const initials = parts.slice(0, 3).map((part) => part.charAt(0).toUpperCase());
	return initials.join("");
}

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
