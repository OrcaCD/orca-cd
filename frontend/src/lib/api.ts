import useSWR, { type SWRConfiguration } from "swr";

export const API_BASE = "/api/v1";

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
	const parsedError = parseErrorMessage(body);
	if (parsedError) {
		return parsedError;
	}

	return fallbackErrorMessage(res);
}

async function fetchWrapper<JSON = any>(input: RequestInfo, init?: RequestInit): Promise<JSON> {
	const res = await fetch(input, { ...init });
	if (!res.ok) {
		throw new Error(await getErrorMessage(res));
	}
	return (await res.json()) as JSON;
}

export async function fetcher<JSON = any>(
	url: string,
	method: "GET" | "POST" | "PUT" | "DELETE",
	data?: any,
) {
	return await fetchWrapper<JSON>(API_BASE + url, {
		method,
		headers: data ? { "Content-Type": "application/json" } : undefined,
		body: data ? JSON.stringify(data) : undefined,
	});
}

export const useFetch = <JSON = any>(url: string, config?: SWRConfiguration) => {
	return useSWR<JSON>(API_BASE + url, fetchWrapper, config);
};
