// oxlint-disable react/no-children-prop
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { toast } from "sonner";
import { useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { useAuth, updateProfile } from "@/lib/auth";
import { AlertTriangleIcon } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { m } from "@/lib/paraglide/messages";
import { getLocale, locales, toLocale, type Locale } from "@/lib/paraglide/runtime";
import { setLocale as setAppLocale } from "@/lib/i18n";

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
	const router = useRouter();
	const [isSubmitting, setIsSubmitting] = useState(false);
	const [isChangingLanguage, setIsChangingLanguage] = useState(false);

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

	function getLocaleLabel(locale: Locale): string {
		switch (locale) {
			case "de":
				return m.languageGerman();
			case "en":
				return m.languageEnglish();
			default:
				return locale;
		}
	}

	async function handleLanguageChange(value: string) {
		const nextLocale = toLocale(value);
		if (!nextLocale || nextLocale === getLocale()) {
			return;
		}

		setIsChangingLanguage(true);
		try {
			await setAppLocale(nextLocale, { reload: true });
		} catch (err) {
			toast.error(err instanceof Error ? err.message : m.failedUpdateLanguage());
		}

		setIsChangingLanguage(false);
		await router.invalidate();
	}

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

			<Card>
				<CardHeader>
					<CardTitle>{m.language()}</CardTitle>
					<CardDescription>{m.languageDescription()}</CardDescription>
				</CardHeader>
				<CardContent>
					<div className="max-w-xs space-y-2">
						<Label htmlFor="language-select">{m.language()}</Label>
						<Select
							value={getLocale()}
							onValueChange={handleLanguageChange}
							disabled={isChangingLanguage}
						>
							<SelectTrigger id="language-select">
								<SelectValue placeholder={m.selectLanguage()} />
							</SelectTrigger>
							<SelectContent>
								{locales.map((locale) => (
									<SelectItem key={locale} value={locale}>
										{getLocaleLabel(locale)}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>
				</CardContent>
			</Card>
		</div>
	);
}
