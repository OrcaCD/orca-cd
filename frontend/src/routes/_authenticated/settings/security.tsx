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

export const Route = createFileRoute("/_authenticated/settings/security")({
	component: SecuritySettingsPage,
	head: () => ({
		meta: [
			{
				title: "Settings - Security",
			},
		],
	}),
});

const passwordSchema = z
	.object({
		currentPassword: z.string().min(1, "Current password is required"),
		newPassword: z
			.string()
			.min(8, "New password must be at least 8 characters")
			.max(128, "New password must be at most 128 characters"),
		confirmPassword: z.string().min(1, "Please confirm your new password"),
	})
	.refine((data) => data.newPassword === data.confirmPassword, {
		message: "Passwords do not match",
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
				tabIndex={-1}
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
	const [showPasswords, setShowPasswords] = useState(false);
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
				toast.success("Password updated successfully");
				form.reset();
			} catch (err) {
				toast.error(err instanceof Error ? err.message : "Failed to update password");
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h1 className="text-2xl font-bold">Security</h1>
				<p className="text-muted-foreground text-sm">Manage your password.</p>
			</div>

			<Card>
				<CardHeader>
					<CardTitle>Change Password</CardTitle>
					{isReady && !isLocal && (
						<CardDescription>
							<Alert className="mt-2 border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-50">
								<AlertTriangleIcon />
								<AlertTitle>Managed Profile</AlertTitle>
								<AlertDescription>
									Password management is not available for SSO accounts.
								</AlertDescription>
							</Alert>
						</CardDescription>
					)}
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
								name="currentPassword"
								children={(field) => {
									const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
									return (
										<Field data-invalid={isInvalid}>
											<Label htmlFor={field.name}>Current Password</Label>
											<PasswordInput
												id={field.name}
												name={field.name}
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => field.handleChange(e.target.value)}
												showPassword={showPasswords}
												onToggle={() => setShowPasswords(!showPasswords)}
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
											<Label htmlFor={field.name}>New Password</Label>
											<PasswordInput
												id={field.name}
												name={field.name}
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => field.handleChange(e.target.value)}
												showPassword={showPasswords}
												onToggle={() => setShowPasswords(!showPasswords)}
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
											<Label htmlFor={field.name}>Confirm New Password</Label>
											<PasswordInput
												id={field.name}
												name={field.name}
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => field.handleChange(e.target.value)}
												showPassword={showPasswords}
												onToggle={() => setShowPasswords(!showPasswords)}
												autoComplete="new-password"
												disabled={!isLocal}
											/>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</Field>
									);
								}}
							/>
							<Field>
								<Button type="submit" disabled={isSubmitting || !isLocal}>
									{isSubmitting ? "Updating..." : "Update Password"}
								</Button>
							</Field>
						</FieldGroup>
					</form>
				</CardContent>
			</Card>
		</div>
	);
}
