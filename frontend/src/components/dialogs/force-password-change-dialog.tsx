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
import fetcher, { API_BASE } from "@/lib/api";
import { useAuth } from "@/lib/auth";

const changePasswordSchema = z
	.object({
		currentPassword: z.string().min(1, "Current password is required"),
		newPassword: z
			.string()
			.min(8, "New password must be at least 8 characters")
			.max(128, "New password must be at most 128 characters"),
		confirmPassword: z.string().min(1, "Please confirm your new password"),
	})
	.refine((value) => value.newPassword === value.confirmPassword, {
		message: "Passwords do not match",
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
				await fetcher(`${API_BASE}/auth/change-password`, {
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({
						currentPassword: value.currentPassword,
						newPassword: value.newPassword,
					}),
				});
				await refreshAuth();
				form.reset();
				toast.success("Password updated");
			} catch (err) {
				toast.error(err instanceof Error ? err.message : "Failed to update password");
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
					<DialogTitle>Password Change Required</DialogTitle>
					<DialogDescription>
						Your administrator reset your password. Update it now to continue.
					</DialogDescription>
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
										<Label htmlFor={field.name}>Current Password</Label>
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
										<Label htmlFor={field.name}>New Password</Label>
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
										<Label htmlFor={field.name}>Confirm New Password</Label>
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
								Sign Out
							</Button>
							<Button type="submit" disabled={isSubmitting}>
								{isSubmitting ? "Updating..." : "Update Password"}
							</Button>
						</div>
					</FieldGroup>
				</form>
			</DialogContent>
		</Dialog>
	);
}
