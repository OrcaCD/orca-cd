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
import { useState } from "react";
import { fetchSetup, login, register } from "@/lib/api";
import { useAuth } from "@/lib/auth";

const searchSchema = z.object({
	redirect: z.string().optional(),
});

export const Route = createFileRoute("/login")({
	validateSearch: searchSchema,
	beforeLoad: ({ context }) => {
		if (context.auth.isAuthenticated) {
			throw redirect({ to: "/" });
		}
	},
	loader: async () => {
		const { needsSetup } = await fetchSetup();
		return { needsSetup };
	},
	component: LoginComponent,
});

const loginSchema = z.object({
	username: z.string().min(1, "Username is required"),
	password: z.string().min(1, "Password is required"),
});

const registerSchema = z
	.object({
		username: z
			.string()
			.min(3, "Username must be at least 3 characters")
			.max(64, "Username must be at most 64 characters"),
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
	const { needsSetup } = Route.useLoaderData();
	const { redirect: redirectTo } = Route.useSearch();
	const navigate = useNavigate();
	const { setAuth } = useAuth();
	const [showPassword, setShowPassword] = useState(false);
	const [isLoading, setIsLoading] = useState(false);

	const loginForm = useForm({
		defaultValues: { username: "", password: "" },
		validators: { onSubmit: loginSchema },
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			try {
				const auth = await login(value.username, value.password);
				setAuth(auth);
				await navigate({ to: redirectTo ?? "/" });
			} catch (err) {
				toast.error(err instanceof Error ? err.message : "Login failed");
			} finally {
				setIsLoading(false);
			}
		},
	});

	const registerForm = useForm({
		defaultValues: { username: "", password: "", confirmPassword: "" },
		validators: { onSubmit: registerSchema },
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			try {
				const auth = await register(value.username, value.password);
				setAuth(auth);
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
		<div className="flex min-h-svh w-full items-center justify-center p-6 md:p-10">
			<div className="w-full max-w-sm">
				<div className="flex flex-col gap-6">
					<div className="flex items-center gap-2 self-center font-medium">
						<div className="flex size-8 items-center justify-center rounded-md text-primary-foreground">
							<img src="/assets/logo-dark-1024.png" alt="OrcaCD Logo" />
						</div>
						OrcaCD
					</div>
					{needsSetup ? (
						<Card>
							<CardHeader className="text-center">
								<CardTitle className="text-xl">Create your account</CardTitle>
								<CardDescription>Set up the first admin account to get started</CardDescription>
							</CardHeader>
							<CardContent>
								<form
									onSubmit={async (e) => {
										e.preventDefault();
										await registerForm.handleSubmit();
									}}
								>
									<FieldGroup>
										<registerForm.Field
											name="username"
											children={(field) => {
												const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
												return (
													<Field data-invalid={isInvalid}>
														<Label htmlFor={field.name}>Username</Label>
														<Input
															id={field.name}
															name={field.name}
															value={field.state.value}
															onBlur={field.handleBlur}
															onChange={(e) => field.handleChange(e.target.value)}
															type="text"
															placeholder="admin"
															required
															autoCapitalize="none"
															autoComplete="username"
															autoCorrect="off"
															autoFocus
														/>
														{isInvalid && <FieldError errors={field.state.meta.errors} />}
													</Field>
												);
											}}
										/>
										<registerForm.Field
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
										<registerForm.Field
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
															showPassword={showPassword}
															onToggle={() => setShowPassword(!showPassword)}
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
							</CardContent>
						</Card>
					) : (
						<Card>
							<CardHeader className="text-center">
								<CardTitle className="text-xl">Login to your account</CardTitle>
							</CardHeader>
							<CardContent>
								<form
									onSubmit={async (e) => {
										e.preventDefault();
										await loginForm.handleSubmit();
									}}
								>
									<FieldGroup>
										<loginForm.Field
											name="username"
											children={(field) => {
												const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
												return (
													<Field data-invalid={isInvalid}>
														<Label htmlFor={field.name}>Username</Label>
														<Input
															id={field.name}
															name={field.name}
															value={field.state.value}
															onBlur={field.handleBlur}
															onChange={(e) => field.handleChange(e.target.value)}
															type="text"
															placeholder="admin"
															required
															autoCapitalize="none"
															autoComplete="username"
															autoCorrect="off"
															autoFocus
														/>
														{isInvalid && <FieldError errors={field.state.meta.errors} />}
													</Field>
												);
											}}
										/>
										<loginForm.Field
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
							</CardContent>
						</Card>
					)}
				</div>
			</div>
		</div>
	);
}

function PasswordInput({
	showPassword,
	onToggle,
	...props
}: React.ComponentProps<typeof Input> & {
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
