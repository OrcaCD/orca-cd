import { createFileRoute } from "@tanstack/react-router";
import { useForm } from "@tanstack/react-form";
import { useState } from "react";
import z from "zod";
import { RegistryListInput } from "@/components/release-age/registry-list-input";
import { ReleaseAgeInput } from "@/components/release-age/release-age-input";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/toast";
import { useFetch } from "@/lib/api";
import { m } from "@/lib/paraglide/messages";
import {
	maxReleaseAgeSeconds,
	updateGlobalReleaseAgeSettings,
	type GlobalReleaseAgeSettings,
} from "@/lib/release-age";

export const Route = createFileRoute("/_authenticated/admin/release-age")({
	component: AdminReleaseAgePage,
	head: () => ({
		meta: [
			{
				title: `${m.admin()} - ${m.adminReleaseAge()}`,
			},
		],
	}),
});

const globalSchema = z.object({
	minimumSeconds: z.number().int().min(0).max(maxReleaseAgeSeconds, m.validationReleaseAgeRange()),
	allowedRegistries: z.array(z.string()),
});

function AdminReleaseAgePage() {
	const { data, mutate } = useFetch<GlobalReleaseAgeSettings>("/settings/release-age");
	if (!data) {
		return <Skeleton className="h-64 w-full" />;
	}
	return (
		<GlobalReleaseAgeForm
			settings={data}
			onUpdated={(updated) => mutate(updated, { revalidate: false })}
		/>
	);
}

function GlobalReleaseAgeForm({
	settings,
	onUpdated,
}: {
	settings: GlobalReleaseAgeSettings;
	onUpdated: (settings: GlobalReleaseAgeSettings) => Promise<unknown>;
}) {
	const [isSubmitting, setIsSubmitting] = useState(false);
	const form = useForm({
		defaultValues: settings,
		validators: {
			onSubmit: globalSchema,
		},
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				const updated = await updateGlobalReleaseAgeSettings(value);
				await onUpdated(updated);
				form.reset(updated);
				toast.add({ type: "success", title: m.adminReleaseAgeUpdated() });
			} catch (err) {
				toast.add({
					type: "error",
					title: err instanceof Error ? err.message : m.adminReleaseAgeUpdateFailed(),
				});
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.adminReleaseAge()}</CardTitle>
				<CardDescription>{m.adminReleaseAgeDescription()}</CardDescription>
			</CardHeader>
			<Separator />
			<CardContent>
				<form
					onSubmit={async (e) => {
						e.preventDefault();
						await form.handleSubmit();
					}}
				>
					<FieldGroup className="max-w-xl">
						<form.Field name="minimumSeconds">
							{(field) => {
								const isInvalid = !field.state.meta.isValid;
								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>{m.releaseAgeMinimum()}</Label>
										<ReleaseAgeInput
											id={field.name}
											seconds={field.state.value}
											onBlur={field.handleBlur}
											onChange={(seconds) => field.handleChange(seconds)}
										/>
										<p className="text-muted-foreground text-xs">{m.releaseAgeMinimumHint()}</p>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						</form.Field>

						<form.Field name="allowedRegistries">
							{(field) => (
								<Field>
									<Label htmlFor="global-release-age-registry">
										{m.releaseAgeAllowedRegistries()}
									</Label>
									<p className="text-muted-foreground text-xs">
										{m.adminReleaseAgeRegistriesHint()}
									</p>
									<RegistryListInput
										id="global-release-age-registry"
										value={field.state.value}
										onChange={(registries) => field.handleChange(registries)}
									/>
								</Field>
							)}
						</form.Field>

						<div className="flex flex-wrap gap-2 pt-2">
							<Button type="submit" disabled={isSubmitting}>
								{isSubmitting ? m.savingDots() : m.saveChanges()}
							</Button>
							<Button
								type="button"
								variant="outline"
								onClick={() => form.reset()}
								disabled={isSubmitting}
							>
								{m.cancel()}
							</Button>
						</div>
					</FieldGroup>
				</form>
			</CardContent>
		</Card>
	);
}
