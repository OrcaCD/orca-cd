// oxlint-disable react/no-children-prop
import { createFileRoute } from "@tanstack/react-router";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { toast } from "sonner";
import { useState, type ComponentProps } from "react";
import { AlertTriangleIcon, Eye, EyeOff } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { useAuth, updatePassword } from "@/lib/auth";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { m } from "@/lib/paraglide/messages";

export const Route = createFileRoute("/_authenticated/settings/security")({
	component: SecuritySettingsPage,
	head: () => ({
		meta: [
			{
				title: m.settingsSecurityHeadTitle(),
			},
		],
	}),
});

const passwordSchema = z
	.object({
		currentPassword: z.string().min(1, m.validationCurrentPasswordRequired()),
		newPassword: z
			.string()
			.min(8, m.validationNewPasswordMinLength())
			.max(128, m.validationNewPasswordMaxLength()),
		confirmPassword: z.string().min(1, m.validationConfirmNewPasswordRequired()),
	})
	.refine((data) => data.newPassword === data.confirmPassword, {
		message: m.validationPasswordsMustMatch(),
		path: ["confirmPassword"],
	});

function PasswordInput({
	showPassword,
	onToggle,
	...props
}: ComponentProps<"input"> & { showPassword: boolean; onToggle: () => void }) {
	return (
		<div className="relative">
			<Input type={showPassword ? "text" : "password"} {...props} />
			<Button
				type="button"
				variant="ghost"
				size="icon"
				className="absolute right-0 top-0 h-full px-3 py-2 hover:bg-transparent"
				onClick={onToggle}
				aria-label={showPassword ? m.hidePassword() : m.showPassword()}
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

function SecuritySettingsPage() {
	const { auth } = useAuth();
	const [showCurrentPasswords, setShowCurrentPasswords] = useState(false);
	const [showNewPasswords, setShowNewPasswords] = useState(false);
	const [showConfirmPasswords, setShowConfirmPasswords] = useState(false);
	const [isSubmitting, setIsSubmitting] = useState(false);

	const isLocal = auth.profile?.isLocal ?? false;
	const isReady = !auth.isLoading;

	const form = useForm({
		defaultValues: {
			currentPassword: "",
			newPassword: "",
			confirmPassword: "",
		},
		validators: { onSubmit: passwordSchema },
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				await updatePassword(value.currentPassword, value.newPassword);
				toast.success(m.passwordUpdatedSuccessfully());
				form.reset();
			} catch (err) {
				toast.error(err instanceof Error ? err.message : m.failedUpdatePassword());
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h1 className="text-2xl font-bold">{m.security()}</h1>
				<p className="text-muted-foreground text-sm">{m.securityDescription()}</p>
			</div>

			<Card>
				<CardHeader>
					<CardTitle>{m.changePassword()}</CardTitle>
					{isReady && !isLocal && (
						<CardDescription>
							<Alert className="mt-2 border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-50">
								<AlertTriangleIcon />
								<AlertTitle>{m.managedProfile()}</AlertTitle>
								<AlertDescription>{m.passwordManagementUnavailableForSso()}</AlertDescription>
							</Alert>
						</CardDescription>
					)}
				</CardHeader>
				<CardContent>
					<form
						className="max-w-xl"
						onSubmit={async (e) => {
							e.preventDefault();
							await form.handleSubmit();
						}}
					>
						<FieldGroup>
							<form.Field
								name="currentPassword"
								children={(field) => {
									const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
									return (
										<Field data-invalid={isInvalid}>
											<Label htmlFor={field.name}>{m.currentPassword()}</Label>
											<PasswordInput
												id={field.name}
												name={field.name}
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => field.handleChange(e.target.value)}
												showPassword={showCurrentPasswords}
												onToggle={() => setShowCurrentPasswords(!showCurrentPasswords)}
												autoComplete="current-password"
												disabled={!isLocal}
											/>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</Field>
									);
								}}
							/>
							<form.Field
								name="newPassword"
								children={(field) => {
									const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
									return (
										<Field data-invalid={isInvalid}>
											<Label htmlFor={field.name}>{m.newPassword()}</Label>
											<PasswordInput
												id={field.name}
												name={field.name}
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => field.handleChange(e.target.value)}
												showPassword={showNewPasswords}
												onToggle={() => setShowNewPasswords(!showNewPasswords)}
												autoComplete="new-password"
												disabled={!isLocal}
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
											<Label htmlFor={field.name}>{m.confirmNewPassword()}</Label>
											<PasswordInput
												id={field.name}
												name={field.name}
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => field.handleChange(e.target.value)}
												showPassword={showConfirmPasswords}
												onToggle={() => setShowConfirmPasswords(!showConfirmPasswords)}
												autoComplete="new-password"
												disabled={!isLocal}
											/>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</Field>
									);
								}}
							/>
							<Field>
								<Button type="submit" disabled={isSubmitting || !isLocal} className="max-w-fit">
									{isSubmitting ? m.updatingDots() : m.updatePassword()}
								</Button>
							</Field>
						</FieldGroup>
					</form>
				</CardContent>
			</Card>
		</div>
	);
}
