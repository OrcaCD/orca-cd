import type { AuthState } from "./auth";

const API_BASE = "/api/v1";

interface SetupResponse {
	needsSetup: boolean;
}

interface TokenResponse {
	token: string;
}

interface ErrorResponse {
	error: string;
}

export async function fetchSetup(): Promise<SetupResponse> {
	const res = await fetch(`${API_BASE}/auth/setup`);
	if (!res.ok) {
		throw new Error("Failed to check setup status");
	}
	return res.json();
}

export async function login(username: string, password: string): Promise<AuthState> {
	const res = await fetch(`${API_BASE}/auth/login`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ username, password }),
	});

	if (!res.ok) {
		const body: ErrorResponse = await res.json();
		throw new Error(body.error ?? "Login failed");
	}

	const { token }: TokenResponse = await res.json();
	return decodeAndStore(token);
}

export async function register(username: string, password: string): Promise<AuthState> {
	const res = await fetch(`${API_BASE}/auth/register`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ username, password }),
	});

	if (!res.ok) {
		const body: ErrorResponse = await res.json();
		throw new Error(body.error ?? "Registration failed");
	}

	const { token }: TokenResponse = await res.json();
	return decodeAndStore(token);
}

function decodeAndStore(token: string): AuthState {
	const payload = JSON.parse(atob(token.split(".")[1]));
	const auth: AuthState = {
		isAuthenticated: true,
		token,
		userId: payload.uid,
		username: payload.usr,
	};
	localStorage.setItem("auth_token", token);
	return auth;
}
