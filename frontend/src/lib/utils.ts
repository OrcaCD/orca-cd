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
