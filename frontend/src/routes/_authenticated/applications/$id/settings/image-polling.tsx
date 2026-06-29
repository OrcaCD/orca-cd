import { createFileRoute } from "@tanstack/react-router";
import { m } from "@/lib/paraglide/messages";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import z from "zod";
import { updateApplication, type Application } from "@/lib/applications";
import { useState } from "react";
import { useForm, useStore } from "@tanstack/react-form";
import { toast } from "sonner";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { useFetch } from "@/lib/api";

export const Route = createFileRoute("/_authenticated/applications/$id/settings/image-polling")({
	component: ImagePollingPage,
	head: () => ({
		meta: [
			{
				title: `${m.navApplications()} - ${m.settings()}`,
			},
		],
	}),
});

const applicationSchema = z.object({
	name: z
		.string()
		.trim()
		.min(1, m.validationApplicationNameRequired())
		.max(128, m.validationApplicationNameMaxLength()),
	icon: z.string().max(128, m.validationApplicationIconMaxLegth()),
	repositoryId: z.string().min(1, m.validationRepositoryRequired()),
	agentId: z.string().min(1, m.validationAgentRequired()),
	branch: z
		.string()
		.trim()
		.min(1, m.validationBranchRequired())
		.max(256, m.validationBranchMaxLength()),
	path: z
		.string()
		.trim()
		.min(1, m.validationPathRequired())
		.max(512, m.validationPathMaxLength())
		.refine((val) => val.endsWith(".yml") || val.endsWith(".yaml"), {
			message: m.validationPathMustBeYAML(),
		}),
	imagePollEnabled: z.boolean(),
	imagePollIntervalSeconds: z.number().int().min(60, m.validationImagePollIntervalMin()),
	imagePollDeleteOldImages: z.boolean(),
});

function ImagePollingPage() {
	const { id } = Route.useParams();
	const { data: application } = useFetch<Application>(`/applications/${id}`);
	const [, setIsSubmitting] = useState(false);
	const [, setOpen] = useState(false);

	const form = useForm({
		defaultValues: {
			name: application?.name ?? "",
			icon: application?.icon ?? "",
			repositoryId: application?.repositoryId ?? "",
			agentId: application?.agentId ?? "",
			branch: application?.branch ?? "",
			path: application?.path ?? "",
			imagePollEnabled: application?.imagePollEnabled ?? false,
			imagePollIntervalSeconds: application?.imagePollIntervalSeconds || 120,
			imagePollDeleteOldImages: application?.imagePollDeleteOldImages ?? false,
		},
		validators: {
			onSubmit: applicationSchema,
		},
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				await updateApplication(application!.id, value);
				toast.success(m.applicationUpdated());
				setOpen(false);
			} catch (err) {
				toast.error(err instanceof Error ? err.message : m.failedSaveApplication());
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	const imagePollEnabled = useStore(form.store, (state) => state.values.imagePollEnabled);

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h1 className="text-2xl font-bold">{m.imagePollSectionTitle()}</h1>
				<p className="text-muted-foreground text-sm">{m.imagePollDescription()}</p>
			</div>
			<Card>
				<CardHeader>
					<CardTitle>{m.imagePollSectionTitle()}</CardTitle>
				</CardHeader>
				<Separator />
				<CardContent>
					<form
						className="overflow-y-auto"
						onSubmit={async (e) => {
							e.preventDefault();
							await form.handleSubmit();
						}}
					>
						<FieldGroup className="max-w-xl">
							<form.Field name="imagePollEnabled">
								{(field) => (
									<Field>
										<div className="flex items-start gap-3">
											<Switch
												id={field.name}
												checked={field.state.value}
												onCheckedChange={(checked) => field.handleChange(checked)}
											/>
											<div className="space-y-1">
												<Label htmlFor={field.name}>{m.imagePollEnabled()}</Label>
												<p className="text-muted-foreground text-xs">
													{m.imagePollEnabledDescription()}
												</p>
											</div>
										</div>
									</Field>
								)}
							</form.Field>
							{imagePollEnabled && (
								<>
									<form.Field name="imagePollIntervalSeconds">
										{(field) => {
											const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
											return (
												<Field data-invalid={isInvalid}>
													<Label htmlFor={field.name}>{m.imagePollIntervalSeconds()}</Label>
													<Input
														id={field.name}
														type="number"
														min={60}
														value={field.state.value}
														onBlur={field.handleBlur}
														onChange={(e) => field.handleChange(Number(e.target.value))}
													/>
													<p className="text-muted-foreground text-xs">
														{m.imagePollIntervalHint()}
													</p>
													{isInvalid && <FieldError errors={field.state.meta.errors} />}
												</Field>
											);
										}}
									</form.Field>

									<form.Field name="imagePollDeleteOldImages">
										{(field) => (
											<Field>
												<div className="flex items-start gap-2">
													<Checkbox
														id={field.name}
														checked={field.state.value}
														onCheckedChange={(checked) => field.handleChange(checked === true)}
													/>
													<div className="space-y-1">
														<Label htmlFor={field.name}>{m.imagePollDeleteOldImages()}</Label>
														<p className="text-muted-foreground text-xs">
															{m.imagePollDeleteOldImagesDescription()}
														</p>
													</div>
												</div>
											</Field>
										)}
									</form.Field>
								</>
							)}
						</FieldGroup>
					</form>
				</CardContent>
			</Card>
		</div>
	);
}
