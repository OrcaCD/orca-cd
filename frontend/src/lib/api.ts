const API_BASE = "/api/v1";

interface SetupResponse {
	needsSetup: boolean;
}

export interface ProfileResponse {
	id: string;
	name: string;
	email: string;
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

export async function fetchProfile(): Promise<ProfileResponse> {
	const res = await fetch(`${API_BASE}/auth/profile`, {
		credentials: "include",
	});
	if (!res.ok) {
		throw new Error("Not authenticated");
	}
	return res.json();
}

export async function login(email: string, password: string): Promise<void> {
	const res = await fetch(`${API_BASE}/auth/login`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		credentials: "include",
		body: JSON.stringify({ email, password }),
	});

	if (!res.ok) {
		const body: ErrorResponse = await res.json();
		throw new Error(body.error ?? "Login failed");
	}
}

export async function register(name: string, email: string, password: string): Promise<void> {
	const res = await fetch(`${API_BASE}/auth/register`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		credentials: "include",
		body: JSON.stringify({ name, email, password }),
	});

	if (!res.ok) {
		const body: ErrorResponse = await res.json();
		throw new Error(body.error ?? "Registration failed");
	}
}
