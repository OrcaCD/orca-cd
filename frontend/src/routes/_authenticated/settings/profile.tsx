// oxlint-disable react/no-children-prop
import { createFileRoute } from "@tanstack/react-router";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { toast } from "sonner";
import { useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { useAuth, updateProfile } from "@/lib/auth";
import { AlertTriangleIcon } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

export const Route = createFileRoute("/_authenticated/settings/profile")({
	component: ProfileSettingsPage,
	head: () => ({
		meta: [
			{
				title: "Settings - Profile",
			},
		],
	}),
});

const profileSchema = z.object({
	name: z
		.string()
		.min(3, "Name must be at least 3 characters")
		.max(64, "Name must be at most 64 characters"),
	email: z.email("Must be a valid email address"),
});

function ProfileSettingsPage() {
	const { auth, refreshAuth } = useAuth();
	const [isSubmitting, setIsSubmitting] = useState(false);

	const form = useForm({
		defaultValues: {
			name: auth.profile?.name ?? "",
			email: auth.profile?.email ?? "",
		},
		validators: { onSubmit: profileSchema },
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				await updateProfile(value);
				await refreshAuth();
				toast.success("Profile updated");
			} catch (err) {
				toast.error(err instanceof Error ? err.message : "Failed to update profile");
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	const isLocal = auth.profile?.isLocal ?? false;
	const isReady = !auth.isLoading;

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h1 className="text-2xl font-bold">Profile</h1>
				<p className="text-muted-foreground text-sm">Manage your name and email address.</p>
			</div>

			<Card>
				<CardHeader>
					<CardTitle>Personal Information</CardTitle>
					{isReady && !isLocal && (
						<CardDescription>
							<Alert className="mt-2 border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-50">
								<AlertTriangleIcon />
								<AlertTitle>Managed Profile</AlertTitle>
								<AlertDescription>
									Your profile is managed by your identity provider and cannot be edited here.
								</AlertDescription>
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
												disabled={!isLocal}
												autoComplete="name"
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
												type="email"
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => field.handleChange(e.target.value)}
												disabled={!isLocal}
												autoComplete="email"
											/>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</Field>
									);
								}}
							/>
							<Field>
								<Button type="submit" disabled={isSubmitting || !isLocal} className="max-w-fit">
									{isSubmitting ? "Saving..." : "Save Changes"}
								</Button>
							</Field>
						</FieldGroup>
					</form>
				</CardContent>
			</Card>
		</div>
	);
}
