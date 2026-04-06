import { createContext, useCallback, useContext } from "react";
import useSWR from "swr";
import fetcher, { API_BASE } from "./api";

export interface AuthState {
	isAuthenticated: boolean;
	isLoading: boolean;
	isAdmin: boolean;
	profile: Profile | null;
}

interface AuthContextValue {
	auth: AuthState;
	logout: () => Promise<void>;
	refreshAuth: () => Promise<void>;
}

interface Profile {
	id: string;
	name: string;
	email: string;
	role: string;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
	const { data, isLoading, mutate } = useSWR<Profile>(`${API_BASE}/auth/profile`, fetcher, {
		shouldRetryOnError: false,
	});

	const auth: AuthState = {
		isLoading,
		profile: data || null,
		isAuthenticated: !!data,
		isAdmin: data?.role === "admin",
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
