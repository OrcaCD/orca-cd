import { createContext, useCallback, useContext, useState } from "react";

export interface AuthState {
	isAuthenticated: boolean;
	token: string | null;
	userId: string | null;
	username: string | null;
}

const UNAUTHENTICATED: AuthState = {
	isAuthenticated: false,
	token: null,
	userId: null,
	username: null,
};

interface AuthContextValue {
	auth: AuthState;
	setAuth: (auth: AuthState) => void;
	logout: () => void;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

function loadAuthFromStorage(): AuthState {
	const token = localStorage.getItem("auth_token");
	if (!token) {
		return UNAUTHENTICATED;
	}

	try {
		const payload = JSON.parse(atob(token.split(".")[1]));
		// Check expiry
		if (payload.exp && payload.exp * 1000 < Date.now()) {
			localStorage.removeItem("auth_token");
			return UNAUTHENTICATED;
		}
		return {
			isAuthenticated: true,
			token,
			userId: payload.uid,
			username: payload.usr,
		};
	} catch {
		localStorage.removeItem("auth_token");
		return UNAUTHENTICATED;
	}
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
	const [auth, setAuth] = useState<AuthState>(loadAuthFromStorage);

	const logout = useCallback(() => {
		localStorage.removeItem("auth_token");
		setAuth(UNAUTHENTICATED);
	}, []);

	return <AuthContext.Provider value={{ auth, setAuth, logout }}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
	const ctx = useContext(AuthContext);
	if (!ctx) {
		throw new Error("useAuth must be used within AuthProvider");
	}
	return ctx;
}
