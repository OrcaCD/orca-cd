// oxlint-disable react/no-children-prop
import { Pencil, Plus } from "lucide-react";
import { useState } from "react";
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
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useFetch } from "@/lib/api";
import type { ApplicationListItem } from "@/lib/applications";
import {
	createNotification,
	isHttpUrl,
	normalizeNotificationApplicationIds,
	type Notification,
	type NotificationType,
	notificationTypes,
	updateNotification,
} from "@/lib/notifications";
import { m } from "@/lib/paraglide/messages";
import {
	Combobox,
	ComboboxContent,
	ComboboxEmpty,
	ComboboxInput,
	ComboboxItem,
	ComboboxList,
} from "@/components/ui/combobox";
import {
	buildDiscordNotificationConfig,
	DiscordAvatarUrlField,
	DiscordBotNameField,
	DiscordThreadIdField,
	DiscordWebhookUrlField,
	parseDiscordBuilderValues,
	parseDiscordWebhookUrl,
} from "./discord-notification-builder";
import { Item, ItemContent, ItemDescription, ItemTitle } from "../ui/item";

const notificationSchema = z
	.object({
		name: z
			.string()
			.trim()
			.min(1, m.validationNotificationNameRequired())
			.max(128, m.validationNotificationNameMaxLength()),
		type: z.enum(notificationTypes),
		discordWebhookUrl: z.string().trim(),
		discordBotName: z.string().trim(),
		discordAvatarUrl: z.string().trim(),
		discordThreadId: z.string().trim(),
		enabled: z.boolean(),
		enableByDefault: z.boolean(),
		applicationIds: z.array(z.string()),
	})
	.superRefine((value, ctx) => {
		if (value.type === "discord") {
			if (value.discordWebhookUrl === "") {
				ctx.addIssue({
					code: "custom",
					path: ["discordWebhookUrl"],
					message: m.validationNotificationWebhookUrlRequired(),
				});
			} else if (!parseDiscordWebhookUrl(value.discordWebhookUrl)) {
				ctx.addIssue({
					code: "custom",
					path: ["discordWebhookUrl"],
					message: m.validationNotificationWebhookUrlInvalid(),
				});
			}

			if (value.discordAvatarUrl !== "" && !isHttpUrl(value.discordAvatarUrl)) {
				ctx.addIssue({
					code: "custom",
					path: ["discordAvatarUrl"],
					message: m.validationNotificationAvatarUrlInvalid(),
				});
			}

			if (value.discordThreadId !== "" && !/^\d+$/.test(value.discordThreadId)) {
				ctx.addIssue({
					code: "custom",
					path: ["discordThreadId"],
					message: m.validationNotificationThreadIdInvalid(),
				});
			}
		}
	});

type NotificationFormValues = z.infer<typeof notificationSchema>;

function buildNotificationConfig(value: NotificationFormValues): string {
	if (value.type === "discord") {
		const config = buildDiscordNotificationConfig({
			discordWebhookUrl: value.discordWebhookUrl,
			discordBotName: value.discordBotName,
			discordAvatarUrl: value.discordAvatarUrl,
			discordThreadId: value.discordThreadId,
		});
		if (!config) {
			throw new Error(m.validationNotificationWebhookUrlInvalid());
		}

		return config;
	}

	return "";
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
	const discordBuilderValues = parseDiscordBuilderValues(notification?.config);

	const { data: applications } = useFetch<ApplicationListItem[]>("/applications");

	const form = useForm({
		defaultValues: {
			name: notification?.name ?? "",
			type: (notification?.type ?? "discord") as NotificationType,
			discordWebhookUrl: discordBuilderValues.discordWebhookUrl,
			discordBotName: discordBuilderValues.discordBotName,
			discordAvatarUrl: discordBuilderValues.discordAvatarUrl,
			discordThreadId: discordBuilderValues.discordThreadId,
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
					config: buildNotificationConfig(value),
					enabled: value.enabled,
					enableByDefault: value.enableByDefault,
					applicationIds: normalizeNotificationApplicationIds(value.applicationIds),
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
		<Dialog open={open} onOpenChange={setOpen} modal={false}>
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

						<form.Subscribe selector={(state) => state.values.type}>
							{(type) =>
								type === "discord" ? (
									<>
										<form.Field
											name="discordWebhookUrl"
											children={(field) => <DiscordWebhookUrlField field={field} />}
										/>

										<form.Field
											name="discordBotName"
											children={(field) => <DiscordBotNameField field={field} />}
										/>

										<form.Field
											name="discordAvatarUrl"
											children={(field) => <DiscordAvatarUrlField field={field} />}
										/>

										<form.Field
											name="discordThreadId"
											children={(field) => <DiscordThreadIdField field={field} />}
										/>
									</>
								) : null
							}
						</form.Subscribe>

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
									<Combobox
										items={applications}
										multiple
										value={field.state.value}
										onValueChange={field.handleChange}
									>
										<ComboboxInput placeholder={m.selectApplications()} />
										<ComboboxContent>
											<ComboboxEmpty>{m.noApplicationsAvailable()}</ComboboxEmpty>
											<ComboboxList>
												{(item) => (
													<ComboboxItem key={item.id} value={item.id}>
														<Item className="p-0">
															<ItemContent>
																<ItemTitle className="whitespace-nowrap">{item.name}</ItemTitle>
																<ItemDescription>
																	{item.repositoryName} / {item.agentName}
																</ItemDescription>
															</ItemContent>
														</Item>
													</ComboboxItem>
												)}
											</ComboboxList>
										</ComboboxContent>
									</Combobox>
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
