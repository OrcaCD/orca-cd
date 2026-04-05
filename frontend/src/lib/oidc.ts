import fetcher, { API_BASE } from "./api";

export interface AuthProviderInfo {
	id: string;
	name: string;
}

export interface OIDCProviderDetail {
	id: string;
	name: string;
	issuerUrl: string;
	clientId: string;
	scopes: string;
	enabled: boolean;
	createdAt: string;
	updatedAt: string;
}

export interface CreateOIDCProviderRequest {
	name: string;
	issuerUrl: string;
	clientId: string;
	clientSecret: string;
	scopes?: string;
	enabled?: boolean;
}

export interface UpdateOIDCProviderRequest {
	name: string;
	issuerUrl: string;
	clientId: string;
	clientSecret?: string;
	scopes?: string;
	enabled?: boolean;
}

export function fetchOIDCProviders(): Promise<OIDCProviderDetail[]> {
	return fetcher<OIDCProviderDetail[]>(`${API_BASE}/admin/oidc-providers`);
}

export function fetchOIDCProvider(id: string): Promise<OIDCProviderDetail> {
	return fetcher<OIDCProviderDetail>(`${API_BASE}/admin/oidc-providers/${id}`);
}

export function createOIDCProvider(data: CreateOIDCProviderRequest): Promise<OIDCProviderDetail> {
	return fetcher<OIDCProviderDetail>(`${API_BASE}/admin/oidc-providers`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(data),
	});
}

export function updateOIDCProvider(
	id: string,
	data: UpdateOIDCProviderRequest,
): Promise<OIDCProviderDetail> {
	return fetcher<OIDCProviderDetail>(`${API_BASE}/admin/oidc-providers/${id}`, {
		method: "PUT",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(data),
	});
}

export async function deleteOIDCProvider(id: string): Promise<void> {
	await fetcher(`${API_BASE}/admin/oidc-providers/${id}`, { method: "DELETE" });
}
