// oxlint-disable react/no-children-prop
import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { fetcher } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { m } from "@/lib/paraglide/messages";

const changePasswordSchema = z
	.object({
		currentPassword: z.string().min(1, m.validationCurrentPasswordRequired()),
		newPassword: z
			.string()
			.min(12, m.validationNewPasswordMinLength())
			.max(128, m.validationNewPasswordMaxLength())
			.superRefine((val, ctx) => {
				if (!/[A-Z]/.test(val))
					ctx.addIssue({ code: "custom", message: m.validationNewPasswordMissingUppercase() });
				if (!/[a-z]/.test(val))
					ctx.addIssue({ code: "custom", message: m.validationNewPasswordMissingLowercase() });
				if (!/[0-9]/.test(val))
					ctx.addIssue({ code: "custom", message: m.validationNewPasswordMissingNumber() });
				if (!/[^A-Za-z0-9]/.test(val))
					ctx.addIssue({ code: "custom", message: m.validationNewPasswordMissingSpecial() });
			}),
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
				toast.success(m.passwordUpdated());
			} catch (err) {
				toast.error(err instanceof Error ? err.message : m.failedUpdatePassword());
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	if (!auth.isAuthenticated || !auth.passwordChangeRequired) {
		return null;
	}

	return (
		<Dialog open>
			<DialogContent
				showCloseButton={false}
				onEscapeKeyDown={(event) => event.preventDefault()}
				onPointerDownOutside={(event) => event.preventDefault()}
				className="sm:max-w-md"
			>
				<DialogHeader>
					<DialogTitle>{m.forcePasswordChangeRequired()}</DialogTitle>
					<DialogDescription>{m.forcePasswordChangeDescription()}</DialogDescription>
				</DialogHeader>

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
			</DialogContent>
		</Dialog>
	);
}
