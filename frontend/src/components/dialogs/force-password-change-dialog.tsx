// oxlint-disable react/no-children-prop
import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { toast } from "@/components/ui/toast";
import { Button } from "@/components/ui/button";
import {
	ResponsiveDialog,
	ResponsiveDialogContent,
	ResponsiveDialogDescription,
	ResponsiveDialogHeader,
	ResponsiveDialogTitle,
} from "@/components/responsive-dialog";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { fetcher } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { m } from "@/lib/paraglide/messages";
import { passwordStrengthRefine } from "@/lib/utils";

const changePasswordSchema = z
	.object({
		currentPassword: z.string().min(1, m.validationCurrentPasswordRequired()),
		newPassword: z
			.string()
			.min(12, m.validationNewPasswordMinLength())
			.max(128, m.validationNewPasswordMaxLength())
			.superRefine(
				passwordStrengthRefine({
					uppercase: m.validationNewPasswordMissingUppercase(),
					lowercase: m.validationNewPasswordMissingLowercase(),
					number: m.validationNewPasswordMissingNumber(),
					special: m.validationNewPasswordMissingSpecial(),
				}),
			),
		confirmPassword: z.string().min(1, m.validationConfirmNewPasswordRequired()),
	})
	.refine((value) => value.newPassword === value.confirmPassword, {
		message: m.validationPasswordsMustMatch(),
		path: ["confirmPassword"],
	});

export default function ForcePasswordChangeDialog() {
	const { auth, refreshAuth, logout } = useAuth();
	const navigate = useNavigate();
	const [isSubmitting, setIsSubmitting] = useState(false);

	const form = useForm({
		defaultValues: {
			currentPassword: "",
			newPassword: "",
			confirmPassword: "",
		},
		validators: {
			onSubmit: changePasswordSchema,
		},
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				await fetcher("/auth/change-password", "POST", {
					currentPassword: value.currentPassword,
					newPassword: value.newPassword,
				});
				await refreshAuth();
				form.reset();
				toast.add({ type: "success", title: m.passwordUpdated() });
			} catch (err) {
				toast.add({
					type: "error",
					title: err instanceof Error ? err.message : m.failedUpdatePassword(),
				});
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	if (!auth.isAuthenticated || !auth.passwordChangeRequired) {
		return null;
	}

	return (
		<ResponsiveDialog open disablePointerDismissal={true}>
			<ResponsiveDialogContent showCloseButton={false} className="sm:max-w-md">
				<ResponsiveDialogHeader>
					<ResponsiveDialogTitle>{m.forcePasswordChangeRequired()}</ResponsiveDialogTitle>
					<ResponsiveDialogDescription>
						{m.forcePasswordChangeDescription()}
					</ResponsiveDialogDescription>
				</ResponsiveDialogHeader>

				<form
					onSubmit={async (event) => {
						event.preventDefault();
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
										<Input
											id={field.name}
											type="password"
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											autoComplete="current-password"
											autoFocus
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
										<Input
											id={field.name}
											type="password"
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
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
										<Label htmlFor={field.name}>{m.confirmNewPassword()}</Label>
										<Input
											id={field.name}
											type="password"
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											autoComplete="new-password"
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>

						<div className="flex gap-2 pt-2">
							<Button
								type="button"
								variant="outline"
								onClick={async () => {
									await logout();
									await navigate({ to: "/login" });
								}}
								disabled={isSubmitting}
							>
								{m.signOut()}
							</Button>
							<Button type="submit" disabled={isSubmitting}>
								{isSubmitting ? m.updatingDots() : m.updatePassword()}
							</Button>
						</div>
					</FieldGroup>
				</form>
			</ResponsiveDialogContent>
		</ResponsiveDialog>
	);
}
