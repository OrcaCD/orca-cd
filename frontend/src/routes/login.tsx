// oxlint-disable react/no-children-prop
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { z } from "zod";
import { useForm } from "@tanstack/react-form";
import { Separator } from "@/components/ui/separator";
import { useState } from "react";
import { useAuth } from "@/lib/auth";
import type { AuthProviderInfo } from "@/lib/oidc";
import { API_BASE, fetcher } from "@/lib/api";
import { getSetupStatus } from "@/lib/setup";
import { m } from "@/lib/paraglide/messages";
import { AuthErrorBanner, AuthLayout } from "@/components/auth/auth-layout";
import { PasswordInput } from "@/components/auth/password-input";

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
		const { needsSetup, providers, localAuthEnabled } = await getSetupStatus();
		if (needsSetup) {
			throw redirect({ to: "/register" });
		}

		return { providers, localAuthEnabled };
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
	const { providers, localAuthEnabled } = Route.useLoaderData();
	const { error } = Route.useSearch();
	const loginErrorMessage = getLoginErrorMessage(error);

	return (
		<AuthLayout>
			<LoginForm
				providers={providers}
				localAuthEnabled={localAuthEnabled}
				loginErrorMessage={loginErrorMessage}
			/>
		</AuthLayout>
	);
}

function LoginForm({
	providers,
	localAuthEnabled,
	loginErrorMessage,
}: {
	providers: AuthProviderInfo[];
	localAuthEnabled: boolean;
	loginErrorMessage: string | null;
}) {
	const navigate = useNavigate();
	const { refreshAuth } = useAuth();
	const [showPassword, setShowPassword] = useState(false);
	const [isLoading, setIsLoading] = useState(false);
	const [submitError, setSubmitError] = useState<string | null>(null);

	const loginSchema = z.object({
		email: z.email(m.validationInvalidEmail()).trim(),
		password: z.string().min(1, m.validationPasswordRequired()),
	});

	async function login(email: string, password: string): Promise<void> {
		await fetcher("/auth/login", "POST", {
			email,
			password,
		});
	}

	const form = useForm({
		defaultValues: { email: "", password: "" },
		validators: { onSubmit: loginSchema },
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			setSubmitError(null);
			try {
				await login(value.email, value.password);
				await refreshAuth();
				await navigate({ to: "/" });
			} catch (err) {
				setSubmitError(err instanceof Error ? err.message : m.toastLoginFailed());
			} finally {
				setIsLoading(false);
			}
		},
	});

	const hasProviders = providers.length > 0;

	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-col items-center gap-1 text-center">
				<h1 className="text-2xl font-bold">{m.loginToYourAccount()}</h1>
				<p className="text-sm text-balance text-muted-foreground">{m.chooseAuthMethod()}</p>
			</div>

			{loginErrorMessage && <AuthErrorBanner message={loginErrorMessage} />}

			{localAuthEnabled && (
				<form
					onSubmit={async (e) => {
						e.preventDefault();
						await form.handleSubmit();
					}}
				>
					<FieldGroup>
						<form.Field
							name="email"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>{m.email()}</Label>
										<Input
											id={field.name}
											name={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											type="email"
											placeholder={m.emailPlaceholder()}
											required
											autoComplete="email"
											autoFocus
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>
						<form.Field
							name="password"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>{m.password()}</Label>
										<PasswordInput
											id={field.name}
											name={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											showPassword={showPassword}
											onToggle={() => setShowPassword(!showPassword)}
											autoComplete="current-password"
											placeholder={m.passwordPlaceholder()}
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>
						<Field>
							<Button type="submit" className="w-full" disabled={isLoading}>
								{isLoading ? m.loggingIn() : m.login()}
							</Button>
						</Field>
					</FieldGroup>
				</form>
			)}
			{localAuthEnabled && submitError && <AuthErrorBanner message={submitError} />}
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
							className="w-full"
							nativeButton={false}
							render={
								<a href={`${API_BASE}/auth/oidc/${provider.id}/authorize`}>
									{m.continueWith({ providerName: provider.name })}
								</a>
							}
						></Button>
					))}
				</div>
			)}
			{!localAuthEnabled && !hasProviders && (
				<p className="text-center text-sm text-muted-foreground">{m.noLoginMethodsAvailable()}</p>
			)}
		</div>
	);
}
