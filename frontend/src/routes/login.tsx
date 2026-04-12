// oxlint-disable react/no-children-prop
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { z } from "zod";
import { useForm } from "@tanstack/react-form";
import { toast } from "sonner";
import { Eye, EyeOff } from "lucide-react";
import { Separator } from "@/components/ui/separator";
import { useState, type ComponentProps } from "react";
import { useAuth } from "@/lib/auth";
import type { AuthProviderInfo } from "@/lib/oidc";
import { API_BASE, fetcher } from "@/lib/api";

const loginSearchSchema = z.object({
	error: z.string().optional(),
});

const oidcErrorMessages: Record<string, string> = {
	access_denied: "Sign-in was canceled or denied by your identity provider.",
	invalid_callback: "The authentication callback was invalid. Please try again.",
	missing_state: "The sign-in session expired. Please start sign-in again.",
	provider_not_found: "The selected identity provider is unavailable.",
	authentication_failed: "Authentication failed. Please try again.",
	email_not_verified: "Your identity provider did not confirm a verified email address.",
	signup_disabled: "Automatic account creation is disabled for this provider.",
	internal_error: "A server error occurred during sign-in. Please try again.",
	account_creation_failed: "Your account could not be created. Please contact an administrator.",
	account_linking_failed: "Your account could not be linked. Please contact an administrator.",
	account_update_failed: "Your account could not be updated. Please contact an administrator.",
	provider_email_conflict:
		"Another account is already linked to this identity provider with the same email. Please contact an administrator.",
	token_generation_failed: "Sign-in failed while creating your session. Please try again.",
};

function getLoginErrorMessage(error?: string): string | null {
	if (!error) {
		return null;
	}

	return oidcErrorMessages[error] ?? "Authentication failed. Please try again.";
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
				title: "Login",
			},
		],
	}),
});

const loginSchema = z.object({
	email: z.email("Invalid email address").trim(),
	password: z.string().min(1, "Password is required"),
});

const registerSchema = z
	.object({
		name: z
			.string()
			.trim()
			.min(3, "Name must be at least 3 characters")
			.max(64, "Name must be at most 64 characters"),
		email: z.email("Invalid email address").trim(),
		password: z
			.string()
			.min(8, "Password must be at least 8 characters")
			.max(128, "Password must be at most 128 characters"),
		confirmPassword: z.string().min(1, "Please confirm your password"),
	})
	.refine((data) => data.password === data.confirmPassword, {
		message: "Passwords do not match",
		path: ["confirmPassword"],
	});

function LoginComponent() {
	const { needsSetup, providers, localAuthEnabled } = Route.useLoaderData();
	const { error } = Route.useSearch();
	const loginErrorMessage = getLoginErrorMessage(error);

	return (
		<div className="flex min-h-svh w-full items-center justify-center p-6 md:p-10">
			<div className="w-full max-w-sm">
				<div className="flex flex-col gap-6">
					<div className="flex items-center gap-2 self-center font-medium">
						<div className="flex size-8 items-center justify-center rounded-md text-primary-foreground">
							<img src="/assets/logo-dark.svg" alt="OrcaCD Logo" />
						</div>
						OrcaCD
					</div>
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
		</div>
	);
}

function RegisterForm({ loginErrorMessage }: { loginErrorMessage: string | null }) {
	const navigate = useNavigate();
	const { refreshAuth } = useAuth();
	const [showPassword, setShowPassword] = useState(false);
	const [showConfirmPassword, setShowConfirmPassword] = useState(false);
	const [isLoading, setIsLoading] = useState(false);

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
				toast.success("Account created successfully");
				await navigate({ to: "/" });
			} catch (err) {
				toast.error(err instanceof Error ? err.message : "Registration failed");
			} finally {
				setIsLoading(false);
			}
		},
	});

	return (
		<Card>
			<CardHeader className="text-center">
				<CardTitle className="text-xl">Create your account</CardTitle>
				<CardDescription>Set up the first admin account to get started</CardDescription>
			</CardHeader>
			<CardContent>
				<div className="flex flex-col gap-4">
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
											<Label htmlFor={field.name}>Name</Label>
											<Input
												id={field.name}
												name={field.name}
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => field.handleChange(e.target.value)}
												type="text"
												placeholder="Admin"
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
											<Label htmlFor={field.name}>Email</Label>
											<Input
												id={field.name}
												name={field.name}
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => field.handleChange(e.target.value)}
												type="email"
												placeholder="admin@example.com"
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
											<Label htmlFor={field.name}>Password</Label>
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
											<Label htmlFor={field.name}>Confirm Password</Label>
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
									{isLoading ? "Creating account..." : "Create Account"}
								</Button>
							</Field>
						</FieldGroup>
					</form>
				</div>
			</CardContent>
		</Card>
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
				toast.error(err instanceof Error ? err.message : "Login failed");
			} finally {
				setIsLoading(false);
			}
		},
	});

	const hasProviders = providers.length > 0;

	return (
		<Card>
			<CardHeader className="text-center">
				<CardTitle className="text-xl">Login to your account</CardTitle>
			</CardHeader>
			<CardContent>
				<div className="flex flex-col gap-4">
					{loginErrorMessage && <LoginErrorBanner message={loginErrorMessage} />}
					{hasProviders && (
						<div className="flex flex-col gap-2">
							{providers.map((provider) => (
								<Button key={provider.id} variant="outline" className="w-full" asChild>
									<a href={`${API_BASE}/auth/oidc/${provider.id}/authorize`}>
										Continue with {provider.name}
									</a>
								</Button>
							))}
						</div>
					)}
					{hasProviders && localAuthEnabled && (
						<div className="flex items-center gap-4">
							<Separator className="flex-1" />
							<span className="text-muted-foreground text-xs uppercase">or</span>
							<Separator className="flex-1" />
						</div>
					)}
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
												<Label htmlFor={field.name}>Email</Label>
												<Input
													id={field.name}
													name={field.name}
													value={field.state.value}
													onBlur={field.handleBlur}
													onChange={(e) => field.handleChange(e.target.value)}
													type="email"
													placeholder="admin@example.com"
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
												<Label htmlFor={field.name}>Password</Label>
												<PasswordInput
													id={field.name}
													name={field.name}
													value={field.state.value}
													onBlur={field.handleBlur}
													onChange={(e) => field.handleChange(e.target.value)}
													showPassword={showPassword}
													onToggle={() => setShowPassword(!showPassword)}
													autoComplete="current-password"
												/>
												{isInvalid && <FieldError errors={field.state.meta.errors} />}
											</Field>
										);
									}}
								/>
								<Field>
									<Button type="submit" className="w-full" disabled={isLoading}>
										{isLoading ? "Logging in..." : "Login"}
									</Button>
								</Field>
							</FieldGroup>
						</form>
					)}
					{!localAuthEnabled && !hasProviders && (
						<p className="text-center text-sm text-muted-foreground">
							No login methods are currently available. Please contact your administrator.
						</p>
					)}
				</div>
			</CardContent>
		</Card>
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
				aria-label={showPassword ? "Hide password" : "Show password"}
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
