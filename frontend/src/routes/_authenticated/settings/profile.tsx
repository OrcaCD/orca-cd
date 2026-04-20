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
import { m } from "@/lib/paraglide/messages";

export const Route = createFileRoute("/_authenticated/settings/profile")({
	component: ProfileSettingsPage,
	head: () => ({
		meta: [
			{
				title: m.settingsProfileHeadTitle(),
			},
		],
	}),
});

const profileSchema = z.object({
	name: z.string().trim().min(3, m.validationNameMinLength()).max(64, m.validationNameMaxLength()),
	email: z.email(m.validationMustBeValidEmailAddress()).trim(),
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
				toast.success(m.profileUpdated());
			} catch (err) {
				toast.error(err instanceof Error ? err.message : m.failedUpdateProfile());
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
				<h1 className="text-2xl font-bold">{m.profile()}</h1>
				<p className="text-muted-foreground text-sm">{m.profileDescription()}</p>
			</div>

			<Card>
				<CardHeader>
					<CardTitle>{m.personalInformation()}</CardTitle>
					{isReady && !isLocal && (
						<CardDescription>
							<Alert className="mt-2 border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-50">
								<AlertTriangleIcon />
								<AlertTitle>{m.managedProfile()}</AlertTitle>
								<AlertDescription>{m.managedProfileReadOnlyDescription()}</AlertDescription>
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
											<Label htmlFor={field.name}>{m.name()}</Label>
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
											<Label htmlFor={field.name}>{m.email()}</Label>
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
									{isSubmitting ? m.savingDots() : m.saveChanges()}
								</Button>
							</Field>
						</FieldGroup>
					</form>
				</CardContent>
			</Card>
		</div>
	);
}
