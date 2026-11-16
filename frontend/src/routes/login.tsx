import { createFileRoute, redirect, useNavigate, Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Fingerprint } from "lucide-react";
import { z } from "zod";
import { useState } from "react";
import { useAuth } from "@/lib/auth";
import type { AuthProviderInfo } from "@/lib/oidc";
import { API_BASE, fetcher } from "@/lib/api";
import { getSetupStatus } from "@/lib/setup";
import { m } from "@/lib/paraglide/messages";
import { AuthErrorBanner, AuthLayout, authControlClass } from "@/components/auth/auth-layout";
import { cn } from "@/lib/utils";
import { loginWithPasskey, passkeyErrorMessage, passkeysSupported } from "@/lib/passkeys";

const loginSearchSchema = z.object({
	error: z.string().optional(),
});

function getOidcErrorMessages(): Record<string, string> {
	return {
		access_denied: m.oidcErrorAccessDenied(),
		invalid_callback: m.oidcErrorInvalidCallback(),
		missing_state: m.oidcErrorMissingState(),
		provider_not_found: m.oidcErrorProviderNotFound(),
		authentication_failed: m.oidcErrorAuthenticationFailed(),
		email_not_verified: m.oidcErrorEmailNotVerified(),
		signup_disabled: m.oidcErrorSignupDisabled(),
		internal_error: m.oidcErrorInternalError(),
		account_creation_failed: m.oidcErrorAccountCreationFailed(),
		account_linking_failed: m.oidcErrorAccountLinkingFailed(),
		account_update_failed: m.oidcErrorAccountUpdateFailed(),
		provider_email_conflict: m.oidcErrorProviderEmailConflict(),
		token_generation_failed: m.oidcErrorTokenGenerationFailed(),
		role_sync_failed: m.oidcErrorRoleSyncFailed(),
	};
}

function getLoginErrorMessage(error?: string): string | null {
	if (!error) {
		return null;
	}

	if (!Object.keys(getOidcErrorMessages()).includes(error)) {
		return m.oidcErrorAuthenticationFailed();
	}

	return getOidcErrorMessages()[error];
}

export const Route = createFileRoute("/login")({
	validateSearch: (search) => loginSearchSchema.parse(search),
	beforeLoad: ({ context }) => {
		if (context.auth.isAuthenticated) {
			throw redirect({ to: "/" });
		}
	},
	loader: async () => {
		const { needsSetup, providers, localAuthEnabled, demo } = await getSetupStatus();
		if (needsSetup) {
			throw redirect({ to: "/register" });
		}

		return { providers, localAuthEnabled, demo };
	},
	component: LoginComponent,
	head: () => ({
		meta: [
			{
				title: m.login(),
			},
		],
	}),
});

function LoginComponent() {
	const { providers, localAuthEnabled, demo } = Route.useLoaderData();
	const { error } = Route.useSearch();
	const loginErrorMessage = getLoginErrorMessage(error);

	return (
		<AuthLayout>
			<LoginForm
				providers={providers}
				localAuthEnabled={localAuthEnabled}
				demo={demo}
				loginErrorMessage={loginErrorMessage}
			/>
		</AuthLayout>
	);
}

function LoginForm({
	providers,
	localAuthEnabled,
	demo,
	loginErrorMessage,
}: {
	providers: AuthProviderInfo[];
	localAuthEnabled: boolean;
	demo: boolean;
	loginErrorMessage: string | null;
}) {
	const navigate = useNavigate();
	const { refreshAuth } = useAuth();
	const [isLoading, setIsLoading] = useState(false);
	const [submitError, setSubmitError] = useState<string | null>(null);
	const supported = passkeysSupported();
	const hasProviders = providers.length > 0;

	async function login() {
		setIsLoading(true);
		setSubmitError(null);
		try {
			if (demo) {
				await fetcher("/auth/demo", "POST");
			} else {
				await loginWithPasskey();
			}
			await refreshAuth();
			await navigate({ to: "/" });
		} catch (error) {
			setSubmitError(passkeyErrorMessage(error));
		} finally {
			setIsLoading(false);
		}
	}

	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-col gap-2">
				<h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">
					{m.loginToYourAccount()}
				</h1>
				<p className="text-sm text-balance text-muted-foreground">
					{demo ? m.demoLoginDescription() : m.passkeyLoginDescription()}
				</p>
			</div>
			{loginErrorMessage && <AuthErrorBanner message={loginErrorMessage} />}
			{(localAuthEnabled || demo) && (
				<div className="flex flex-col gap-3">
					<Button
						className={cn(authControlClass, "w-full")}
						disabled={isLoading || (!demo && !supported)}
						onClick={login}
					>
						<Fingerprint data-icon="inline-start" />
						{isLoading ? m.loggingIn() : demo ? m.exploreDemo() : m.signInWithPasskey()}
					</Button>
					{!demo && !supported && <AuthErrorBanner message={m.passkeysUnsupported()} />}
					{!demo && (
						<Link className="text-center text-sm underline underline-offset-4" to="/register">
							{m.setUpPasskey()}
						</Link>
					)}
				</div>
			)}
			{submitError && <AuthErrorBanner message={submitError} />}
			{hasProviders && localAuthEnabled && (
				<div className="flex items-center gap-4">
					<Separator className="flex-1" />
					<span className="text-muted-foreground text-xs uppercase">{m.or()}</span>
					<Separator className="flex-1" />
				</div>
			)}
			{hasProviders && (
				<div className="flex flex-col gap-2">
					{providers.map((provider) => (
						<Button
							key={provider.id}
							variant="outline"
							className={cn(authControlClass, "w-full")}
							nativeButton={false}
							render={
								<a href={`${API_BASE}/auth/oidc/${provider.id}/authorize`}>
									{m.continueWith({ providerName: provider.name })}
								</a>
							}
						/>
					))}
				</div>
			)}
			{!localAuthEnabled && !hasProviders && !demo && (
				<p className="text-center text-sm text-muted-foreground">{m.noLoginMethodsAvailable()}</p>
			)}
		</div>
	);
}
