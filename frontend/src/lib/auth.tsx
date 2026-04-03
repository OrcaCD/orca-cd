import { createContext, useCallback, useContext } from "react";
import useSWR from "swr";
import fetcher, { API_BASE } from "./api";

export interface AuthState {
  isAuthenticated: boolean;
  isLoading: boolean;
  id: string | null;
  name: string | null;
  email: string | null;
}

interface AuthContextValue {
  auth: AuthState;
  logout: () => void;
  refreshAuth: () => Promise<void>;
}

interface ProfileResponse {
  id: string;
  name: string;
  email: string;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const { data, isLoading, mutate } = useSWR<ProfileResponse>(`${API_BASE}/auth/profile`, fetcher);

  const auth: AuthState = isLoading
    ? { isAuthenticated: false, isLoading: true, id: null, name: null, email: null }
    : data
      ? { isAuthenticated: true, isLoading: false, id: data.id, name: data.name, email: data.email }
      : { isAuthenticated: false, isLoading: false, id: null, name: null, email: null };

  const refreshAuth = useCallback(async () => {
    await mutate();
  }, [mutate]);

  const logout = useCallback(() => {
    mutate(undefined, false);
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
