// oxlint-disable react/no-children-prop
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { z } from "zod";
import { useForm } from "@tanstack/react-form";
import { toast } from "@/components/ui/toast";
import { useEffect, useState } from "react";
import { useAuth } from "@/lib/auth";
import { fetcher } from "@/lib/api";
import { getSetupStatus } from "@/lib/setup";
import { m } from "@/lib/paraglide/messages";
import { passwordStrengthRefine } from "@/lib/utils";
import { AuthErrorBanner, AuthLayout } from "@/components/auth/auth-layout";
import { PasswordInput } from "@/components/auth/password-input";

export const Route = createFileRoute("/register")({
	beforeLoad: ({ context }) => {
		if (context.auth.isAuthenticated) {
			throw redirect({ to: "/" });
		}
	},
	loader: async () => {
		const { needsSetup } = await getSetupStatus();
		if (!needsSetup) {
			throw redirect({ to: "/login" });
		}
	},
	component: RegisterComponent,
	head: () => ({
		meta: [
			{
				title: m.createYourAccount(),
			},
		],
	}),
});

function RegisterComponent() {
	const [setupCode, setSetupCode] = useState<string | null>(null);

	return (
		<AuthLayout>
			{setupCode === null ? (
				<SetupCodeForm onVerified={setSetupCode} />
			) : (
				<RegisterAccountForm setupCode={setupCode} />
			)}
		</AuthLayout>
	);
}

const SETUP_CODE_HASH_PARAM = "setup-code";

function SetupCodeForm({ onVerified }: { onVerified: (setupCode: string) => void }) {
	const [isLoading, setIsLoading] = useState(false);
	const [submitError, setSubmitError] = useState<string | null>(null);

	const setupCodeSchema = z.object({
		setupCode: z.string().trim().min(1, m.validationSetupCodeRequired()),
	});

	async function verify(setupCode: string): Promise<void> {
		setIsLoading(true);
		setSubmitError(null);
		try {
			await fetcher("/auth/setup/verify", "POST", { setupCode });
			onVerified(setupCode);
		} catch (err) {
			setSubmitError(err instanceof Error ? err.message : m.toastSetupCodeVerificationFailed());
		} finally {
			setIsLoading(false);
		}
	}

	const form = useForm({
		defaultValues: { setupCode: "" },
		validators: { onSubmit: setupCodeSchema },
		onSubmit: async ({ value }) => {
			await verify(value.setupCode.trim());
		},
	});

	// The hub logs a link with the code in the fragment, which is never sent to a server
	useEffect(() => {
		const code = new URLSearchParams(window.location.hash.slice(1)).get(SETUP_CODE_HASH_PARAM);
		if (!code) {
			return;
		}

		history.replaceState(null, "", window.location.pathname + window.location.search);
		void verify(code.trim());
		// oxlint-disable-next-line react/exhaustive-deps
	}, []);

	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-col items-center gap-1 text-center">
				<h1 className="text-2xl font-bold">{m.enterSetupCode()}</h1>
				<p className="text-sm text-balance text-muted-foreground">
					{m.enterSetupCodeDescription()}
				</p>
			</div>

			<form
				onSubmit={async (e) => {
					e.preventDefault();
					await form.handleSubmit();
				}}
			>
				<FieldGroup>
					<form.Field
						name="setupCode"
						children={(field) => {
							const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
							return (
								<Field data-invalid={isInvalid}>
									<Label htmlFor={field.name}>{m.setupCode()}</Label>
									<Input
										id={field.name}
										name={field.name}
										value={field.state.value}
										onBlur={field.handleBlur}
										onChange={(e) => field.handleChange(e.target.value)}
										type="text"
										placeholder="XXXX-XXXX-XXXX-XXXX"
										className="font-mono uppercase"
										required
										autoComplete="off"
										autoCapitalize="characters"
										spellCheck={false}
										autoFocus
									/>
									{isInvalid && <FieldError errors={field.state.meta.errors} />}
								</Field>
							);
						}}
					/>
					<Field>
						<Button type="submit" className="w-full" disabled={isLoading}>
							{isLoading ? m.verifyingSetupCode() : m.next()}
						</Button>
					</Field>
				</FieldGroup>
			</form>
			{submitError && <AuthErrorBanner message={submitError} />}
		</div>
	);
}

function RegisterAccountForm({ setupCode }: { setupCode: string }) {
	const navigate = useNavigate();
	const { refreshAuth } = useAuth();
	const [showPassword, setShowPassword] = useState(false);
	const [showConfirmPassword, setShowConfirmPassword] = useState(false);
	const [isLoading, setIsLoading] = useState(false);
	const [submitError, setSubmitError] = useState<string | null>(null);

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
				.min(12, m.validationPasswordMinLength())
				.max(128, m.validationPasswordMaxLength())
				.superRefine(
					passwordStrengthRefine({
						uppercase: m.validationPasswordMissingUppercase(),
						lowercase: m.validationPasswordMissingLowercase(),
						number: m.validationPasswordMissingNumber(),
						special: m.validationPasswordMissingSpecial(),
					}),
				),
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
			setupCode,
		});
	}

	const form = useForm({
		defaultValues: { name: "", email: "", password: "", confirmPassword: "" },
		validators: { onSubmit: registerSchema },
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			setSubmitError(null);
			try {
				await register(value.name, value.email, value.password);
				await refreshAuth();
				toast.add({ type: "success", title: m.toastAccountCreated() });
				await navigate({ to: "/" });
			} catch (err) {
				setSubmitError(err instanceof Error ? err.message : m.toastRegistrationFailed());
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
			{submitError && <AuthErrorBanner message={submitError} />}
		</div>
	);
}
