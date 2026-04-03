export const API_BASE = "/api/v1";

export interface ErrorResponse {
  error: string;
}

export default async function fetcher<JSON = any>(
  input: RequestInfo,
  init?: RequestInit,
): Promise<JSON> {
  const res = await fetch(input, init);
  return (await res.json()) as JSON;
}
