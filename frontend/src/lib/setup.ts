import { fetcher } from "@/lib/api";
import type { AuthProviderInfo } from "@/lib/oidc";

export async function getSetupStatus() {
	const data = await fetcher<{
		needsSetup: boolean;
		providers?: AuthProviderInfo[];
		localAuthEnabled?: boolean;
	}>("/auth/setup", "GET");

	return {
		needsSetup: data.needsSetup,
		providers: data.providers ?? [],
		localAuthEnabled: data.localAuthEnabled ?? true,
	};
}
