import { ApiError, fetcher } from "./api";
import type { ApiTokenScope } from "./api-tokens";

// What an MCP client asks for, as the hub checked it
export interface OAuthAuthorizationRequest {
	clientName: string;
	// For apps that name themselves with the URL of their description, the
	// host that publishes it. The hub fetched the name and the redirects from
	// there.
	clientHost?: string;
	// Where the user returns to after answering
	redirectUri: string;
	scopes: { scope: ApiTokenScope; grantable: boolean }[];
	// Whether the user may create API tokens, which the client gets one of
	canAuthorize: boolean;
}

interface OAuthRedirect {
	redirectUrl: string;
}

// The query is passed on as it came, so the hub checks exactly what the
// client sent.
export function getOAuthAuthorizationRequest(query: string): Promise<OAuthAuthorizationRequest> {
	return fetcher<OAuthAuthorizationRequest>(`/oauth/authorize?${query}`);
}

export async function answerOAuthAuthorizationRequest(
	query: string,
	approve: boolean,
	scopes: ApiTokenScope[],
): Promise<string> {
	const { redirectUrl } = await fetcher<OAuthRedirect>("/oauth/authorize", "POST", {
		query,
		approve,
		scopes,
	});
	return redirectUrl;
}

// An invalid request is sent back to the client when the hub knows where to,
// so the client doesn't wait for an answer that never comes.
export function oauthErrorRedirect(error: unknown): string | null {
	if (error instanceof ApiError && typeof error.body.redirectUrl === "string") {
		return error.body.redirectUrl;
	}
	return null;
}

const LOGIN_REDIRECT_KEY = "orca-login-redirect";
const OAUTH_AUTHORIZE_PATH = "/oauth/authorize";

// Remembers the consent page while the user logs in. Session storage outlives
// the redirects of an OIDC login, but not the tab.
export function rememberLoginRedirect(url: string) {
	try {
		sessionStorage.setItem(LOGIN_REDIRECT_KEY, url);
	} catch {
		// Without storage, the user starts over from the client
	}
}

// Returns the page to go back to after logging in, once. Only the consent page
// is allowed, so the value can't send the user anywhere else.
export function takeLoginRedirect(): string | null {
	let url: string | null = null;
	try {
		url = sessionStorage.getItem(LOGIN_REDIRECT_KEY);
		sessionStorage.removeItem(LOGIN_REDIRECT_KEY);
	} catch {
		return null;
	}
	if (url?.startsWith(`${OAUTH_AUTHORIZE_PATH}?`)) {
		return url;
	}
	return null;
}
