import { fetcher } from "./api";

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
	requireVerifiedEmail: boolean;
	autoSignup: boolean;
	groupsClaim: string;
	callbackUrl: string;
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
	requireVerifiedEmail?: boolean;
	autoSignup?: boolean;
	groupsClaim?: string;
}

export interface UpdateOIDCProviderRequest {
	name: string;
	issuerUrl: string;
	clientId: string;
	clientSecret?: string;
	scopes?: string;
	enabled?: boolean;
	requireVerifiedEmail?: boolean;
	autoSignup?: boolean;
	groupsClaim?: string;
}

export function createOIDCProvider(data: CreateOIDCProviderRequest): Promise<OIDCProviderDetail> {
	return fetcher<OIDCProviderDetail>(`/admin/oidc-providers`, "POST", data);
}

export function updateOIDCProvider(
	id: string,
	data: UpdateOIDCProviderRequest,
): Promise<OIDCProviderDetail> {
	return fetcher<OIDCProviderDetail>(`/admin/oidc-providers/${id}`, "PUT", data);
}

export async function deleteOIDCProvider(id: string): Promise<void> {
	await fetcher(`/admin/oidc-providers/${id}`, "DELETE");
}
