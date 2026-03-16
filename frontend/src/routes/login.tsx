import { createFileRoute } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldError, FieldGroup, FieldLabel, FieldSeparator } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import z from "zod";
import { useForm } from "@tanstack/react-form";
import { toast } from "sonner";
import { Eye, EyeOff } from "lucide-react";
import { useState } from "react";

export const Route = createFileRoute("/login")({
	component: LoginComponent,
});

const loginSchema = z.object({
	email: z.email(),
	password: z
		.string()
		.min(8, "Password must be at least 8 characters")
		.max(30, "Password must be at most 30 characters")
		.regex(/[a-z]/, "Password must contain at least one lowercase letter")
		.regex(/[A-Z]/, "Password must contain at least one uppercase letter")
		.regex(/[0-9]/, "Password must contain at least one number")
		.regex(/[^a-zA-Z0-9]/, "Password must contain at least one special character"),
});

function LoginComponent() {
	const [showPassword, setShowPassword] = useState(false);

	const form = useForm({
		defaultValues: {
			email: "",
			password: "",
		},
		validators: {
			onSubmit: loginSchema,
		},
		onSubmit: ({ value }) => {
			toast.success("Form submitted successfully with values: " + JSON.stringify(value));
		},
	});

	return (
		<div className="flex min-h-svh w-full items-center justify-center p-6 md:p-10">
			<div className="w-full max-w-sm">
				<div className="flex flex-col gap-6">
					<a href="#" className="flex items-center gap-2 self-center font-medium">
						<div className="flex size-8 items-center justify-center rounded-md text-primary-foreground">
							<img src="/assets/logo-dark-1024.png" alt="OrcaCD Logo" />
						</div>
						OrcaCD
					</a>
					<Card>
						<CardHeader className="text-center">
							<CardTitle className="text-xl">Login to your account</CardTitle>
						</CardHeader>
						<CardContent>
							<form
								onSubmit={async (e) => {
									e.preventDefault();
									await form.handleSubmit();
								}}
							>
								<FieldGroup>
									<form.Field
										name="email"
										// oxlint-disable-next-line react/no-children-prop
										children={(field) => {
											const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

											return (
												<Field data-invalid={isInvalid}>
													<FieldLabel htmlFor={field.name}>Email</FieldLabel>
													<Input
														id={field.name}
														name={field.name}
														value={field.state.value}
														onBlur={field.handleBlur}
														onChange={(e) => field.handleChange(e.target.value)}
														type="email"
														aria-invalid={isInvalid}
														placeholder="m@example.com"
														required
													/>
													{isInvalid && <FieldError errors={field.state.meta.errors} />}
												</Field>
											);
										}}
									/>

									<form.Field
										name="password"
										// oxlint-disable-next-line react/no-children-prop
										children={(field) => {
											const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

											return (
												<Field data-invalid={isInvalid}>
													<FieldLabel htmlFor={field.name}>Password</FieldLabel>
													<div className="w-full max-w-sm space-y-2">
														<div className="relative">
															<Input
																id={field.name}
																name={field.name}
																value={field.state.value}
																onBlur={field.handleBlur}
																onChange={(e) => field.handleChange(e.target.value)}
																aria-invalid={isInvalid}
																required
																type={showPassword ? "text" : "password"}
															/>
															<Button
																className="absolute top-0 right-0 h-full px-3 hover:bg-transparent"
																onClick={() => setShowPassword(!showPassword)}
																size="icon"
																type="button"
																variant="ghost"
															>
																{showPassword ? (
																	<EyeOff className="h-4 w-4 text-muted-foreground" />
																) : (
																	<Eye className="h-4 w-4 text-muted-foreground" />
																)}
															</Button>
														</div>
													</div>
													{isInvalid && <FieldError errors={field.state.meta.errors} />}
												</Field>
											);
										}}
									/>
									<Field>
										<Button type="submit">Login</Button>
									</Field>
									<FieldSeparator>Or continue with</FieldSeparator>
									<Field>
										<Button variant="outline" type="button">
											Login with SSO
										</Button>
									</Field>
								</FieldGroup>
							</form>
						</CardContent>
					</Card>
				</div>
			</div>
		</div>
	);
}
