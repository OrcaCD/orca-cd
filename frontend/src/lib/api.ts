import useSWR, { type SWRConfiguration } from "swr";
import { m } from "@/lib/paraglide/messages";

export const API_BASE = "/api/v1";

// Plain text body returned by the hub for blocked requests in demo mode
const DEMO_BLOCKED_MESSAGE = "This action is not allowed in demo mode.";

export interface ErrorResponse {
	error: string;
}

function fallbackErrorMessage(res: Response): string {
	if (res.statusText) {
		return res.statusText;
	}

	return `Request failed with status ${res.status}`;
}

function parseErrorMessage(body: string): string | null {
	const trimmed = body.trim();
	if (!trimmed) {
		return null;
	}

	try {
		const parsed = JSON.parse(trimmed) as Partial<Record<"error" | "message", unknown>>;
		const error = parsed.error;
		if (typeof error === "string" && error.trim()) {
			return error;
		}

		const message = parsed.message;
		if (typeof message === "string" && message.trim()) {
			return message;
		}
	} catch {
		// Ignore parse errors and fall back to plain text handling.
	}

	if (trimmed.startsWith("<")) {
		return null;
	}

	return trimmed;
}

export async function getErrorMessage(res: Response): Promise<string> {
	const body = await res.text().catch(() => "");
	if (res.status === 403 && body.trim() === DEMO_BLOCKED_MESSAGE) {
		return m.demoActionNotAllowed();
	}

	const parsedError = parseErrorMessage(body);
	if (parsedError) {
		return parsedError;
	}

	return fallbackErrorMessage(res);
}

export class ApiError extends Error {
	status: number;

	constructor(message: string, status: number) {
		super(message);
		this.name = "ApiError";
		this.status = status;
	}
}

async function fetchWrapper<JSON = any>(input: RequestInfo, init?: RequestInit): Promise<JSON> {
	const res = await fetch(input, { ...init });
	if (!res.ok) {
		throw new ApiError(await getErrorMessage(res), res.status);
	}
	return (await res.json()) as JSON;
}

export async function fetcher<JSON extends Record<string, any> | void = any>(
	url: string,
	method: "GET" | "POST" | "PUT" | "DELETE" = "GET",
	data?: Record<string, any>,
	additionalHeaders?: Record<string, string>,
) {
	const requestURL = url.startsWith(`${API_BASE}/`) ? url : API_BASE + url;
	return await fetchWrapper<JSON>(requestURL, {
		method,
		headers: data
			? { "Content-Type": "application/json", ...additionalHeaders }
			: additionalHeaders,
		body: data ? JSON.stringify(data) : undefined,
	});
}

export const useFetch = <JSON extends Record<string, any> = any>(
	url: string | null,
	config?: SWRConfiguration,
) => {
	const requestURL = url && !/^https?:\/\//.test(url) ? API_BASE + url : url;
	return useSWR<JSON>(requestURL, fetchWrapper, config);
};
