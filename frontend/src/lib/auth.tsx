import { createContext, useCallback, useContext } from "react";
import { API_BASE, fetcher, useFetch } from "./api";

export interface AuthState {
	isAuthenticated: boolean;
	isLoading: boolean;
	isAdmin: boolean;
	passwordChangeRequired: boolean;
	profile: Profile | null;
}

interface AuthContextValue {
	auth: AuthState;
	logout: () => Promise<void>;
	refreshAuth: () => Promise<void>;
}

export interface Profile {
	id: string;
	name: string;
	email: string;
	picture?: string;
	role: string;
	passwordChangeRequired: boolean;
	isLocal: boolean;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
	const { data, isLoading, mutate } = useFetch<Profile>("/auth/profile", {
		shouldRetryOnError: false,
	});

	const auth: AuthState = {
		isLoading,
		profile: data || null,
		isAuthenticated: !!data,
		isAdmin: data?.role === "admin",
		passwordChangeRequired: data?.passwordChangeRequired ?? false,
	};

	const refreshAuth = useCallback(async () => {
		await mutate();
	}, [mutate]);

	const logout = useCallback(async () => {
		await fetch(`${API_BASE}/auth/logout`, { method: "POST" });
		await mutate(undefined, false);
	}, [mutate]);

	return (
		<AuthContext.Provider value={{ auth, logout, refreshAuth }}>{children}</AuthContext.Provider>
	);
}

export function useAuth(): AuthContextValue {
	const ctx = useContext(AuthContext);
	if (!ctx) {
		throw new Error("useAuth must be used within AuthProvider");
	}
	return ctx;
}

export async function updateProfile(data: { name: string; email: string }): Promise<void> {
	await fetcher(`/auth/profile`, "PUT", data);
}

export async function updatePassword(currentPassword: string, newPassword: string): Promise<void> {
	await fetcher(`/auth/change-password`, "PUT", { currentPassword, newPassword });
}
