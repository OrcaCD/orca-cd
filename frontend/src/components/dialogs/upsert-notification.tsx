// oxlint-disable react/no-children-prop
import { Pencil, Plus } from "lucide-react";
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useFetch } from "@/lib/api";
import type { ApplicationListItem } from "@/lib/applications";
import {
	createNotification,
	type Notification,
	type NotificationType,
	notificationTypes,
	updateNotification,
} from "@/lib/notifications";
import { m } from "@/lib/paraglide/messages";

const notificationSchema = z.object({
	name: z
		.string()
		.trim()
		.min(1, m.validationNotificationNameRequired())
		.max(128, m.validationNotificationNameMaxLength()),
	type: z.enum(notificationTypes),
	config: z.string().trim().min(1, m.validationNotificationConfigRequired()),
	enabled: z.boolean(),
	enableByDefault: z.boolean(),
	applicationIds: z.array(z.string()),
});

function normalizeIds(ids: string[]): string[] {
	return Array.from(new Set(ids));
}

export default function UpsertNotificationDialog({
	notification,
	asDropdownItem = false,
}: {
	notification: Notification | null;
	asDropdownItem?: boolean;
}) {
	const isEditing = notification !== null;
	const [open, setOpen] = useState(false);
	const [isSubmitting, setIsSubmitting] = useState(false);

	const { data: applications, isLoading: isLoadingApplications } =
		useFetch<ApplicationListItem[]>("/applications");

	const form = useForm({
		defaultValues: {
			name: notification?.name ?? "",
			type: (notification?.type ?? "discord") as NotificationType,
			config: notification?.config ?? "",
			enabled: notification?.enabled ?? true,
			enableByDefault: notification?.enableByDefault ?? false,
			applicationIds: notification?.applicationIds ?? [],
		},
		validators: {
			onSubmit: notificationSchema,
		},
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				const payload = {
					name: value.name,
					type: value.type,
					config: value.config,
					enabled: value.enabled,
					enableByDefault: value.enableByDefault,
					applicationIds: normalizeIds(value.applicationIds),
				};

				if (notification) {
					await updateNotification(notification.id, payload);
					toast.success(m.notificationUpdated());
				} else {
					await createNotification(payload);
					toast.success(m.notificationCreated());
				}

				setOpen(false);
			} catch (err) {
				toast.error(err instanceof Error ? err.message : m.failedSaveNotification());
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger asChild>
				{asDropdownItem ? (
					<DropdownMenuItem onSelect={(e) => e.preventDefault()}>
						<Pencil className="h-4 w-4" />
						{m.edit()}
					</DropdownMenuItem>
				) : (
					<Button>
						<Plus className="h-4 w-4" />
						{m.addNotification()}
					</Button>
				)}
			</DialogTrigger>

			<DialogContent className="sm:max-w-106.25">
				<DialogHeader>
					<DialogTitle>{isEditing ? m.editNotification() : m.addNotification()}</DialogTitle>
					<DialogDescription>
						{isEditing ? m.editNotificationDescription() : m.addNotificationDescription()}
					</DialogDescription>
				</DialogHeader>

				<form
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
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(event) => field.handleChange(event.target.value)}
											placeholder={m.notificationNamePlaceholder()}
											autoFocus
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>

						<form.Field
							name="type"
							children={(field) => (
								<Field>
									<Label htmlFor={field.name}>{m.columnType()}</Label>
									<Select
										value={field.state.value}
										onValueChange={(value) => field.handleChange(value as NotificationType)}
									>
										<SelectTrigger id={field.name} className="w-full">
											<SelectValue placeholder={m.selectType()} />
										</SelectTrigger>
										<SelectContent>
											<SelectItem value="discord">Discord</SelectItem>
										</SelectContent>
									</Select>
								</Field>
							)}
						/>

						<form.Field
							name="config"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>{m.notificationConfig()}</Label>
										<Textarea
											id={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(event) => field.handleChange(event.target.value)}
											rows={5}
											placeholder={m.notificationConfigPlaceholder()}
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>

						<form.Field
							name="enabled"
							children={(field) => (
								<div className="flex items-center justify-between rounded-md border p-3">
									<div>
										<p className="text-sm font-medium">{m.enabled()}</p>
										<p className="text-xs text-muted-foreground">
											{m.notificationEnabledDescription()}
										</p>
									</div>
									<Switch checked={field.state.value} onCheckedChange={field.handleChange} />
								</div>
							)}
						/>

						<form.Field
							name="enableByDefault"
							children={(field) => (
								<div className="flex items-center justify-between rounded-md border p-3">
									<div>
										<p className="text-sm font-medium">{m.enableByDefault()}</p>
										<p className="text-xs text-muted-foreground">
											{m.enableByDefaultDescription()}
										</p>
									</div>
									<Switch checked={field.state.value} onCheckedChange={field.handleChange} />
								</div>
							)}
						/>

						<form.Field
							name="applicationIds"
							children={(field) => (
								<Field>
									<Label>{m.navApplications()}</Label>
									<div className="max-h-44 space-y-2 overflow-y-auto rounded-md border p-3">
										{isLoadingApplications ? (
											<p className="text-sm text-muted-foreground">{m.loadingDots()}</p>
										) : applications?.length ? (
											applications.map((application) => {
												const isChecked = field.state.value.includes(application.id);

												return (
													<label key={application.id} className="flex items-center gap-2 text-sm">
														<Checkbox
															checked={isChecked}
															onCheckedChange={(checked) => {
																if (checked === true) {
																	field.handleChange([...field.state.value, application.id]);
																	return;
																}

																field.handleChange(
																	field.state.value.filter((id) => id !== application.id),
																);
															}}
														/>
														<span>{application.name}</span>
													</label>
												);
											})
										) : (
											<p className="text-sm text-muted-foreground">{m.noApplicationsAvailable()}</p>
										)}
									</div>
								</Field>
							)}
						/>

						<div className="flex gap-2 pt-2">
							<Button type="submit" disabled={isSubmitting}>
								{isSubmitting
									? m.savingDots()
									: isEditing
										? m.updateNotification()
										: m.addNotification()}
							</Button>
							<Button
								type="button"
								variant="outline"
								disabled={isSubmitting}
								onClick={() => {
									setOpen(false);
									form.reset();
								}}
							>
								{m.cancel()}
							</Button>
						</div>
					</FieldGroup>
				</form>
			</DialogContent>
		</Dialog>
	);
}
