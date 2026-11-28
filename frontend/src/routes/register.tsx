// oxlint-disable react/no-children-prop
import { createFileRoute, redirect, useNavigate, Link } from "@tanstack/react-router";
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
import { cn } from "@/lib/utils";
import { AuthErrorBanner, AuthLayout, authControlClass } from "@/components/auth/auth-layout";
import { registerPasskey, passkeyErrorMessage, passkeysSupported } from "@/lib/passkeys";

export const Route = createFileRoute("/register")({
	beforeLoad: ({ context }) => {
		if (context.auth.isAuthenticated) {
			throw redirect({ to: "/" });
		}
	},
	loader: async () => {
		const { needsSetup, localAuthEnabled, demo } = await getSetupStatus();
		if (demo || (!needsSetup && !localAuthEnabled)) {
			throw redirect({ to: "/login" });
		}
		return { needsSetup };
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
	const { needsSetup } = Route.useLoaderData();
	const [setupCode, setSetupCode] = useState<string | null>(null);

	return (
		<AuthLayout>
			{!needsSetup ? (
				<EnrollmentForm />
			) : setupCode === null ? (
				<SetupCodeForm onVerified={setSetupCode} />
			) : (
				<RegisterAccountForm setupCode={setupCode} />
			)}
		</AuthLayout>
	);
}

const SETUP_CODE_HASH_PARAM = "setup-code";

function getSetupCodeFromHash(): string | null {
	return (
		new URLSearchParams(window.location.hash.slice(1)).get(SETUP_CODE_HASH_PARAM)?.trim() || null
	);
}

function SetupCodeForm({ onVerified }: { onVerified: (setupCode: string) => void }) {
	const [isLoading, setIsLoading] = useState(() => getSetupCodeFromHash() !== null);
	const [submitError, setSubmitError] = useState<string | null>(null);

	const setupCodeSchema = z.object({
		setupCode: z.string().trim().min(1, m.validationSetupCodeRequired()),
	});

	const form = useForm({
		defaultValues: { setupCode: "" },
		validators: { onSubmit: setupCodeSchema },
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			setSubmitError(null);
			const setupCode = value.setupCode.trim();
			try {
				await fetcher("/auth/setup/verify", "POST", { setupCode });
				onVerified(setupCode);
			} catch (err) {
				setSubmitError(err instanceof Error ? err.message : m.toastSetupCodeVerificationFailed());
			} finally {
				setIsLoading(false);
			}
		},
	});

	// The hub logs a link with the code in the fragment, which is never sent to a server
	useEffect(() => {
		const code = getSetupCodeFromHash();
		if (!code) {
			return;
		}

		history.replaceState(null, "", window.location.pathname + window.location.search);

		const verify = async () => {
			try {
				await fetcher("/auth/setup/verify", "POST", { setupCode: code });
				onVerified(code);
			} catch (err) {
				setSubmitError(err instanceof Error ? err.message : m.toastSetupCodeVerificationFailed());
			} finally {
				setIsLoading(false);
			}
		};
		void verify();
	}, [onVerified]);

	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-col gap-2">
				<h1 className="text-3xl font-semibold tracking-tight">{m.enterSetupCode()}</h1>
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
										className={cn(authControlClass, "font-mono uppercase")}
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
						<Button type="submit" className={cn(authControlClass, "w-full")} disabled={isLoading}>
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
	const [isLoading, setIsLoading] = useState(false);
	const [submitError, setSubmitError] = useState<string | null>(null);

	const registerSchema = z.object({
		name: z
			.string()
			.trim()
			.min(3, m.validationNameMinLength())
			.max(64, m.validationNameMaxLength()),
		email: z.email(m.validationInvalidEmail()).trim(),
	});

	const form = useForm({
		defaultValues: { name: "", email: "" },
		validators: { onSubmit: registerSchema },
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			setSubmitError(null);
			try {
				await registerPasskey({ name: value.name, email: value.email, setupCode });
				await refreshAuth();
				toast.add({ type: "success", title: m.toastAccountCreated() });
				await navigate({ to: "/" });
			} catch (err) {
				setSubmitError(passkeyErrorMessage(err));
			} finally {
				setIsLoading(false);
			}
		},
	});

	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-col gap-2">
				<h1 className="text-3xl font-semibold tracking-tight">{m.createYourAccount()}</h1>
				<p className="text-sm text-balance text-muted-foreground">
					{m.createPasskeyAccountDescription()}
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
										className={authControlClass}
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
										className={authControlClass}
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
					<Field>
						<Button
							type="submit"
							className={cn(authControlClass, "w-full")}
							disabled={isLoading || !passkeysSupported()}
						>
							{isLoading ? m.creatingAccount() : m.createPasskey()}
						</Button>
					</Field>
				</FieldGroup>
			</form>
			{!passkeysSupported() && <AuthErrorBanner message={m.passkeysUnsupported()} />}
			{submitError && <AuthErrorBanner message={submitError} />}
		</div>
	);
}

function EnrollmentForm() {
	const navigate = useNavigate();
	const { refreshAuth } = useAuth();
	const [code, setCode] = useState(
		() => new URLSearchParams(window.location.hash.slice(1)).get("enrollment-code") ?? "",
	);
	const [isLoading, setIsLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);
	useEffect(() => {
		const token = new URLSearchParams(window.location.hash.slice(1)).get("enrollment-code");
		if (token) {
			history.replaceState(null, "", window.location.pathname + window.location.search);
		}
	}, []);
	async function enroll(event: React.FormEvent<HTMLFormElement>) {
		event.preventDefault();
		setIsLoading(true);
		setError(null);
		try {
			await registerPasskey({ enrollmentToken: code.trim() });
			await refreshAuth();
			await navigate({ to: "/" });
		} catch (err) {
			setError(passkeyErrorMessage(err));
		} finally {
			setIsLoading(false);
		}
	}
	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-col gap-2">
				<h1 className="text-3xl font-semibold tracking-tight">{m.setUpPasskey()}</h1>
				<p className="text-sm text-muted-foreground">{m.enrollmentDescription()}</p>
			</div>
			<form onSubmit={enroll}>
				<FieldGroup>
					<Field>
						<Label htmlFor="enrollment-code">{m.enrollmentCode()}</Label>
						<Input
							id="enrollment-code"
							value={code}
							onChange={(event) => setCode(event.target.value)}
							required
							maxLength={128}
							autoComplete="off"
							spellCheck={false}
							autoFocus
						/>
					</Field>
					<Field>
						<Button type="submit" disabled={isLoading || !passkeysSupported()} className="w-full">
							{isLoading ? m.creatingAccount() : m.createPasskey()}
						</Button>
					</Field>
				</FieldGroup>
			</form>
			{!passkeysSupported() && <AuthErrorBanner message={m.passkeysUnsupported()} />}
			{error && <AuthErrorBanner message={error} />}
			<Link to="/login" className="text-center text-sm underline underline-offset-4">
				{m.backToLogin()}
			</Link>
		</div>
	);
}
