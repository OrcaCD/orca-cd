import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { fetchProfile } from "./api";

export interface AuthState {
	isAuthenticated: boolean;
	isLoading: boolean;
	id: string | null;
	name: string | null;
	email: string | null;
}

const UNAUTHENTICATED: AuthState = {
	isAuthenticated: false,
	isLoading: false,
	id: null,
	name: null,
	email: null,
};

const LOADING: AuthState = {
	isAuthenticated: false,
	isLoading: true,
	id: null,
	name: null,
	email: null,
};

interface AuthContextValue {
	auth: AuthState;
	setAuth: (auth: AuthState) => void;
	logout: () => void;
	refreshAuth: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
	const [auth, setAuth] = useState<AuthState>(LOADING);

	const refreshAuth = useCallback(async () => {
		try {
			const profile = await fetchProfile();
			setAuth({
				isAuthenticated: true,
				isLoading: false,
				id: profile.id,
				name: profile.name,
				email: profile.email,
			});
		} catch {
			setAuth(UNAUTHENTICATED);
		}
	}, []);

	useEffect(() => {
		refreshAuth();
	}, [refreshAuth]);

	const logout = useCallback(() => {
		setAuth(UNAUTHENTICATED);
	}, []);

	return (
		<AuthContext.Provider value={{ auth, setAuth, logout, refreshAuth }}>
			{children}
		</AuthContext.Provider>
	);
}

export function useAuth(): AuthContextValue {
	const ctx = useContext(AuthContext);
	if (!ctx) {
		throw new Error("useAuth must be used within AuthProvider");
	}
	return ctx;
}
