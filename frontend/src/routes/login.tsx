// oxlint-disable react/no-children-prop
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { z } from "zod";
import { useForm } from "@tanstack/react-form";
import { toast } from "sonner";
import { Eye, EyeOff, FileTextIcon } from "lucide-react";
import { Separator } from "@/components/ui/separator";
import { useState, type ComponentProps } from "react";
import { useAuth } from "@/lib/auth";
import type { AuthProviderInfo } from "@/lib/oidc";
import { API_BASE, fetcher } from "@/lib/api";
import { m } from "@/lib/paraglide/messages";

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
	const { needsSetup, providers, localAuthEnabled } = Route.useLoaderData();
	const { error } = Route.useSearch();
	const loginErrorMessage = getLoginErrorMessage(error);

	return (
		<div className="grid min-h-svh lg:grid-cols-2">
			<div className="flex flex-col gap-4 p-6 md:p-10">
				<div className="flex justify-center gap-2 md:justify-start">
					<div className="flex items-center gap-3 font-medium text-xl">
						<div className="flex size-8 items-center justify-center rounded-md bg-primary text-primary-foreground">
							<img src="/assets/logo-dark.svg" alt="OrcaCD Logo" />
						</div>
						OrcaCD
					</div>
				</div>
				<div className="flex flex-1 items-center justify-center">
					<div className="w-full max-w-xs">
						{needsSetup ? (
							<RegisterForm loginErrorMessage={loginErrorMessage} />
						) : (
							<LoginForm
								providers={providers}
								localAuthEnabled={localAuthEnabled}
								loginErrorMessage={loginErrorMessage}
							/>
						)}
					</div>
				</div>
				<div className="flex items-center gap-1">
					<a
						href="https://github.com/OrcaCD/orca-cd"
						target="_blank"
						rel="noopener noreferrer nofollow"
					>
						<Button variant="ghost" size="icon-lg">
							<img
								src="/assets/icons/github.svg"
								alt={m.githubLogoAlt()}
								className="size-6 dark:invert"
							/>
						</Button>
					</a>
					<a href="https://orcacd.dev" target="_blank" rel="noopener noreferrer nofollow">
						<Button variant="ghost" size="icon-lg">
							<FileTextIcon className="size-6" />
						</Button>
					</a>
				</div>
			</div>
			<div className="relative hidden bg-muted lg:block">
				<img
					src="/assets/wallpaper/lachlan-gowen-lleHA3cpZXo-unsplash.jpg"
					alt={m.loginWallpaperAlt()}
					title={m.loginWallpaperAlt()}
					className="absolute inset-0 h-full w-full object-cover"
				/>
			</div>
		</div>
	);
}

function RegisterForm({ loginErrorMessage }: { loginErrorMessage: string | null }) {
	const navigate = useNavigate();
	const { refreshAuth } = useAuth();
	const [showPassword, setShowPassword] = useState(false);
	const [showConfirmPassword, setShowConfirmPassword] = useState(false);
	const [isLoading, setIsLoading] = useState(false);

	const registerSchema = z
		.object({
			name: z
				.string()
				.trim()
				.min(3, m.validationNameMinLength())
				.max(64, m.validationNameMaxLength()),
			email: z.email(m.validationInvalidEmail()).trim(),
			password: z
				.string()
				.min(8, m.validationPasswordMinLength())
				.max(128, m.validationPasswordMaxLength()),
			confirmPassword: z.string().min(1, m.validationConfirmPasswordRequired()),
		})
		.refine((data) => data.password === data.confirmPassword, {
			message: m.validationPasswordsMustMatch(),
			path: ["confirmPassword"],
		});

	async function register(name: string, email: string, password: string): Promise<void> {
		await fetcher("/auth/register", "POST", {
			name,
			email,
			password,
		});
	}

	const form = useForm({
		defaultValues: { name: "", email: "", password: "", confirmPassword: "" },
		validators: { onSubmit: registerSchema },
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			try {
				await register(value.name, value.email, value.password);
				await refreshAuth();
				toast.success(m.toastAccountCreated());
				await navigate({ to: "/" });
			} catch (err) {
				toast.error(err instanceof Error ? err.message : m.toastRegistrationFailed());
			} finally {
				setIsLoading(false);
			}
		},
	});

	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-col items-center gap-1 text-center">
				<h1 className="text-2xl font-bold">{m.createYourAccount()}</h1>
				<p className="text-sm text-balance text-muted-foreground">
					{m.createAdminAccountDescription()}
				</p>
			</div>

			{loginErrorMessage && <LoginErrorBanner message={loginErrorMessage} />}
			<form
				onSubmit={async (e) => {
					e.preventDefault();
					await form.handleSubmit();
				}}
			>
				<FieldGroup>
					<form.Field
						name="name"
						children={(field) => {
							const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
							return (
								<Field data-invalid={isInvalid}>
									<Label htmlFor={field.name}>{m.name()}</Label>
									<Input
										id={field.name}
										name={field.name}
										value={field.state.value}
										onBlur={field.handleBlur}
										onChange={(e) => field.handleChange(e.target.value)}
										type="text"
										placeholder={m.adminNamePlaceholder()}
										required
										autoComplete="name"
										autoFocus
									/>
									{isInvalid && <FieldError errors={field.state.meta.errors} />}
								</Field>
							);
						}}
					/>
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
										autoComplete="new-password"
									/>
									{isInvalid && <FieldError errors={field.state.meta.errors} />}
								</Field>
							);
						}}
					/>
					<form.Field
						name="confirmPassword"
						children={(field) => {
							const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
							return (
								<Field data-invalid={isInvalid}>
									<Label htmlFor={field.name}>{m.confirmPassword()}</Label>
									<PasswordInput
										id={field.name}
										name={field.name}
										value={field.state.value}
										onBlur={field.handleBlur}
										onChange={(e) => field.handleChange(e.target.value)}
										showPassword={showConfirmPassword}
										onToggle={() => setShowConfirmPassword(!showConfirmPassword)}
										autoComplete="new-password"
									/>
									{isInvalid && <FieldError errors={field.state.meta.errors} />}
								</Field>
							);
						}}
					/>
					<Field>
						<Button type="submit" className="w-full" disabled={isLoading}>
							{isLoading ? m.creatingAccount() : m.createAccount()}
						</Button>
					</Field>
				</FieldGroup>
			</form>
		</div>
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
			try {
				await login(value.email, value.password);
				await refreshAuth();
				await navigate({ to: "/" });
			} catch (err) {
				toast.error(err instanceof Error ? err.message : m.toastLoginFailed());
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

			{loginErrorMessage && <LoginErrorBanner message={loginErrorMessage} />}

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
						<Button key={provider.id} variant="outline" className="w-full" asChild>
							<a href={`${API_BASE}/auth/oidc/${provider.id}/authorize`}>
								{m.continueWith({ providerName: provider.name })}
							</a>
						</Button>
					))}
				</div>
			)}
			{!localAuthEnabled && !hasProviders && (
				<p className="text-center text-sm text-muted-foreground">{m.noLoginMethodsAvailable()}</p>
			)}
		</div>
	);
}

function LoginErrorBanner({ message }: { message: string }) {
	return (
		<div
			role="alert"
			className="rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive"
		>
			{message}
		</div>
	);
}

function PasswordInput({
	showPassword,
	onToggle,
	...props
}: ComponentProps<typeof Input> & {
	showPassword: boolean;
	onToggle: () => void;
}) {
	return (
		<div className="relative">
			<Input {...props} type={showPassword ? "text" : "password"} required />
			<Button
				className="absolute top-0 right-0 h-full px-3 hover:bg-transparent"
				onClick={onToggle}
				size="icon"
				type="button"
				variant="ghost"
				aria-label={showPassword ? m.hidePassword() : m.showPassword()}
				aria-pressed={showPassword}
			>
				{showPassword ? (
					<EyeOff className="h-4 w-4 text-muted-foreground" />
				) : (
					<Eye className="h-4 w-4 text-muted-foreground" />
				)}
			</Button>
		</div>
	);
}
