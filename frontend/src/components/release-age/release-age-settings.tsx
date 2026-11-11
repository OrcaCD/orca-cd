import { useForm, useStore } from "@tanstack/react-form";
import { Info } from "lucide-react";
import { useState } from "react";
import z from "zod";
import { RegistryListInput } from "@/components/release-age/registry-list-input";
import { ReleaseAgeInput } from "@/components/release-age/release-age-input";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { toast } from "@/components/ui/toast";
import { useFetch } from "@/lib/api";
import { updateReleaseAgeSettings, type Application } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import {
	formatReleaseAge,
	maxReleaseAgeSeconds,
	releaseAgeLabel,
	type GlobalReleaseAgeSettings,
} from "@/lib/release-age";

const releaseAgeSchema = z.object({
	useGlobal: z.boolean(),
	seconds: z.number().int().min(0).max(maxReleaseAgeSeconds, m.validationReleaseAgeRange()),
	allowedRegistries: z.array(z.string()),
});

// ReleaseAgeSettings configures the minimum release age of an application, or
// lets it use the global default.
export function ReleaseAgeSettings({
	application,
	onUpdated,
}: {
	application: Application | undefined;
	onUpdated: (application: Application) => Promise<unknown>;
}) {
	const { data: global } = useFetch<GlobalReleaseAgeSettings>("/settings/release-age");
	const [isSubmitting, setIsSubmitting] = useState(false);

	const settings = application?.releaseAge;
	const form = useForm({
		defaultValues: {
			useGlobal: settings ? settings.minimumSeconds === null : true,
			seconds: settings?.minimumSeconds ?? 0,
			allowedRegistries: settings?.allowedRegistries ?? [],
		},
		validators: {
			onSubmit: releaseAgeSchema,
		},
		onSubmit: async ({ value }) => {
			if (!application) {
				return;
			}
			setIsSubmitting(true);
			try {
				const updated = await updateReleaseAgeSettings(application.id, {
					minimumSeconds: value.useGlobal ? null : value.seconds,
					allowedRegistries: value.allowedRegistries,
				});
				// Update the cache first, otherwise the form falls back to the stale defaults after the reset
				await onUpdated(updated);
				form.reset(value);
				toast.add({ type: "success", title: m.applicationUpdated() });
			} catch (err) {
				toast.add({
					type: "error",
					title: err instanceof Error ? err.message : m.failedSaveApplication(),
				});
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	const useGlobal = useStore(form.store, (state) => state.values.useGlobal);

	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.releaseAgeSectionTitle()}</CardTitle>
				<CardDescription>{m.releaseAgeDescription()}</CardDescription>
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
						<form.Field name="useGlobal">
							{(field) => (
								<Field>
									<div className="flex items-start gap-3">
										<Switch
											id={field.name}
											checked={field.state.value}
											onCheckedChange={(checked) => field.handleChange(checked)}
										/>
										<div className="space-y-1">
											<Label htmlFor={field.name}>{m.releaseAgeUseGlobal()}</Label>
											<p className="text-muted-foreground text-xs">
												{m.releaseAgeUseGlobalDescription({
													age: formatReleaseAge(global?.minimumSeconds ?? 0),
												})}
											</p>
										</div>
									</div>
								</Field>
							)}
						</form.Field>

						{!useGlobal && (
							<form.Field name="seconds">
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
						)}

						<div className="flex gap-2 rounded-md bg-muted/50 p-3 text-xs text-muted-foreground">
							<Info className="mt-0.5 h-4 w-4 shrink-0" />
							<div className="space-y-1">
								<p>{m.releaseAgePushTimeInfo()}</p>
								<p>
									{m.releaseAgeLabelInfo()}{" "}
									<code className="font-mono text-foreground">{releaseAgeLabel}</code>
								</p>
							</div>
						</div>

						<form.Field name="allowedRegistries">
							{(field) => (
								<Field>
									<Label htmlFor="release-age-registry">{m.releaseAgeAllowedRegistries()}</Label>
									<p className="text-muted-foreground text-xs">
										{m.releaseAgeAllowedRegistriesHint()}
									</p>
									{global && global.allowedRegistries.length > 0 && (
										<div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
											{m.releaseAgeGlobalRegistries()}
											{global.allowedRegistries.map((registry) => (
												<Badge key={registry} variant="outline" className="font-mono">
													{registry}
												</Badge>
											))}
										</div>
									)}
									<RegistryListInput
										id="release-age-registry"
										value={field.state.value}
										onChange={(registries) => field.handleChange(registries)}
									/>
								</Field>
							)}
						</form.Field>

						<div className="flex flex-wrap gap-2 pt-2">
							<Button type="submit" disabled={isSubmitting || !application}>
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
