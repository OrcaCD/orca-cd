export const API_BASE = "/api/v1";

export interface ErrorResponse {
	error: string;
}

export default async function fetcher<JSON = any>(
	input: RequestInfo,
	init?: RequestInit,
): Promise<JSON> {
	const res = await fetch(input, { credentials: "include", ...init });
	if (!res.ok) {
		const body: ErrorResponse = await res.json().catch(() => ({ error: res.statusText }));
		throw new Error(body.error);
	}
	return (await res.json()) as JSON;
}
