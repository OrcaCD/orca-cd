// oxlint-disable react/no-children-prop
import { Plus } from "lucide-react";
import { Fragment, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { toast } from "sonner";
import { defineStepper } from "@stepperize/react";
import { useStepItemContext, type StepStatus } from "@stepperize/react/primitives";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
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
	type NotificationType,
	notificationTypes,
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
import { cn } from "@/lib/utils";
import {
	buildDiscordNotificationConfig,
	DiscordAvatarUrlField,
	DiscordBotNameField,
	DiscordThreadIdField,
	DiscordWebhookUrlField,
	parseDiscordWebhookUrl,
} from "./discord-notification-builder";
import {
	buildSlackNotificationConfig,
	isValidSlackWebhookUrl,
	SlackWebhookUrlField,
} from "./slack-notification-builder";
import {
	buildGotifyNotificationConfig,
	GotifyAppTokenField,
	GotifyCustomPathField,
	GotifyPriorityField,
	GotifyServerUrlField,
	isValidGotifyAppToken,
	parseGotifyPriority,
} from "./gotify-notification-builder";
import {
	buildWebhookNotificationConfig,
	parseWebhookHeaders,
	WebhookHeadersField,
	WebhookUrlField,
} from "./webhook-notification-builder";
import {
	buildCustomNotificationConfig,
	CustomShoutrrrUrlField,
	isValidShoutrrrUrl,
} from "./custom-notification-builder";
import { Item, ItemContent, ItemDescription, ItemTitle } from "../ui/item";

const { Stepper } = defineStepper({ id: "config" }, { id: "provider" });

const notificationBaseSchema = z.object({
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
	gotifyServerUrl: z.string().trim(),
	gotifyAppToken: z.string().trim(),
	gotifyPriority: z.string().trim(),
	gotifyCustomPath: z.string().trim(),
	slackWebhookUrl: z.string().trim(),
	webhookUrl: z.string().trim(),
	webhookHeaders: z.string(),
	customShoutrrrUrl: z.string().trim(),
	enabled: z.boolean(),
	enableByDefault: z.boolean(),
	applicationIds: z.array(z.string()),
});

const notificationSchema = notificationBaseSchema.superRefine((value, ctx) => {
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

	if (value.type === "gotify") {
		if (value.gotifyServerUrl === "") {
			ctx.addIssue({
				code: "custom",
				path: ["gotifyServerUrl"],
				message: m.validationNotificationGotifyServerUrlRequired(),
			});
		} else if (!isHttpUrl(value.gotifyServerUrl)) {
			ctx.addIssue({
				code: "custom",
				path: ["gotifyServerUrl"],
				message: m.validationNotificationGotifyServerUrlInvalid(),
			});
		}

		if (value.gotifyAppToken === "") {
			ctx.addIssue({
				code: "custom",
				path: ["gotifyAppToken"],
				message: m.validationNotificationGotifyAppTokenRequired(),
			});
		} else if (!isValidGotifyAppToken(value.gotifyAppToken)) {
			ctx.addIssue({
				code: "custom",
				path: ["gotifyAppToken"],
				message: m.validationNotificationGotifyAppTokenInvalid(),
			});
		}

		if (parseGotifyPriority(value.gotifyPriority) === null) {
			ctx.addIssue({
				code: "custom",
				path: ["gotifyPriority"],
				message: m.validationNotificationGotifyPriorityInvalid(),
			});
		}
	}

	if (value.type === "slack") {
		if (value.slackWebhookUrl === "") {
			ctx.addIssue({
				code: "custom",
				path: ["slackWebhookUrl"],
				message: m.validationNotificationSlackWebhookUrlRequired(),
			});
		} else if (!isValidSlackWebhookUrl(value.slackWebhookUrl)) {
			ctx.addIssue({
				code: "custom",
				path: ["slackWebhookUrl"],
				message: m.validationNotificationSlackWebhookUrlInvalid(),
			});
		}
	}

	if (value.type === "webhook") {
		if (value.webhookUrl === "") {
			ctx.addIssue({
				code: "custom",
				path: ["webhookUrl"],
				message: m.validationNotificationWebhookUrlRequired(),
			});
		} else if (!isHttpUrl(value.webhookUrl)) {
			ctx.addIssue({
				code: "custom",
				path: ["webhookUrl"],
				message: m.validationNotificationGenericWebhookUrlInvalid(),
			});
		}

		if (!parseWebhookHeaders(value.webhookHeaders)) {
			ctx.addIssue({
				code: "custom",
				path: ["webhookHeaders"],
				message: m.validationNotificationWebhookHeadersInvalid(),
			});
		}
	}

	if (value.type === "custom") {
		if (value.customShoutrrrUrl === "") {
			ctx.addIssue({
				code: "custom",
				path: ["customShoutrrrUrl"],
				message: m.validationNotificationShoutrrrUrlRequired(),
			});
		} else if (!isValidShoutrrrUrl(value.customShoutrrrUrl)) {
			ctx.addIssue({
				code: "custom",
				path: ["customShoutrrrUrl"],
				message: m.validationNotificationShoutrrrUrlInvalid(),
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

	if (value.type === "slack") {
		const config = buildSlackNotificationConfig({
			slackWebhookUrl: value.slackWebhookUrl,
		});
		if (!config) {
			throw new Error(m.validationNotificationSlackWebhookUrlInvalid());
		}

		return config;
	}

	if (value.type === "gotify") {
		const config = buildGotifyNotificationConfig({
			gotifyServerUrl: value.gotifyServerUrl,
			gotifyAppToken: value.gotifyAppToken,
			gotifyPriority: value.gotifyPriority,
			gotifyCustomPath: value.gotifyCustomPath,
		});
		if (!config) {
			throw new Error(m.validationNotificationGotifyConfigInvalid());
		}

		return config;
	}

	if (value.type === "webhook") {
		const config = buildWebhookNotificationConfig({
			webhookUrl: value.webhookUrl,
			webhookHeaders: value.webhookHeaders,
		});
		if (!config) {
			throw new Error(m.validationNotificationGenericWebhookUrlInvalid());
		}

		return config;
	}

	if (value.type === "custom") {
		const config = buildCustomNotificationConfig({
			customShoutrrrUrl: value.customShoutrrrUrl,
		});
		if (!config) {
			throw new Error(m.validationNotificationShoutrrrUrlInvalid());
		}

		return config;
	}

	return "";
}

// Only used for ReturnType inference — never called at runtime
function useNotificationForm() {
	return useForm({
		defaultValues: {
			name: "",
			type: "discord" as NotificationType,
			discordWebhookUrl: "",
			discordBotName: "",
			discordAvatarUrl: "",
			discordThreadId: "",
			gotifyServerUrl: "",
			gotifyAppToken: "",
			gotifyPriority: "5",
			gotifyCustomPath: "",
			slackWebhookUrl: "",
			webhookUrl: "",
			webhookHeaders: "",
			customShoutrrrUrl: "",
			enabled: true,
			enableByDefault: false,
			applicationIds: [] as string[],
		},
		validators: { onSubmit: notificationSchema },
		// oxlint-disable-next-line no-empty-function
		onSubmit: async () => {},
	});
}
type NotificationFormApi = ReturnType<typeof useNotificationForm>;

const StepperTriggerWrapper = ({ displayNumber }: { displayNumber?: number }) => {
	const item = useStepItemContext();
	const isInactive = item.status === "inactive";
	const number = displayNumber ?? item.index + 1;

	return (
		<Stepper.Trigger
			render={(domProps) => (
				<Button
					className="rounded-full cursor-default"
					variant={isInactive ? "secondary" : "default"}
					size="icon"
					{...domProps}
					onClick={(e) => {
						e.preventDefault();
					}}
				>
					<Stepper.Indicator>{number}</Stepper.Indicator>
				</Button>
			)}
		/>
	);
};

const StepperSeparatorWithStatus = ({
	status,
	isLast,
}: {
	status: StepStatus;
	isLast: boolean;
}) => {
	if (isLast) {
		return null;
	}

	return (
		<Stepper.Separator
			orientation="horizontal"
			data-status={status}
			className="self-center bg-muted data-[status=success]:bg-primary data-disabled:opacity-50 transition-all duration-300 ease-in-out data-[orientation=horizontal]:h-0.5 data-[orientation=horizontal]:min-w-4 data-[orientation=horizontal]:flex-1"
		/>
	);
};

function NotificationConfigStepContent({
	form,
	applications,
}: {
	form: NotificationFormApi;
	applications: ApplicationListItem[] | undefined;
}) {
	return (
		<FieldGroup>
			<form.Field name="name" validators={{ onSubmit: notificationBaseSchema.shape.name }}>
				{(field) => {
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
			</form.Field>

			<form.Field name="type" validators={{ onSubmit: notificationBaseSchema.shape.type }}>
				{(field) => (
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
								{notificationTypes.map((type) => (
									<SelectItem key={type} value={type}>
										{type.charAt(0).toUpperCase() + type.slice(1)}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</Field>
				)}
			</form.Field>

			<form.Field name="enabled">
				{(field) => (
					<div className="flex items-center justify-between rounded-md border p-3 gap-4">
						<div>
							<p className="text-sm font-medium">{m.enabled()}</p>
							<p className="text-xs text-muted-foreground">{m.notificationEnabledDescription()}</p>
						</div>
						<Switch checked={field.state.value} onCheckedChange={field.handleChange} />
					</div>
				)}
			</form.Field>

			<form.Field name="enableByDefault">
				{(field) => (
					<div className="flex items-center justify-between rounded-md border p-3 gap-4">
						<div>
							<p className="text-sm font-medium">{m.enableByDefault()}</p>
							<p className="text-xs text-muted-foreground">{m.enableByDefaultDescription()}</p>
						</div>
						<Switch checked={field.state.value} onCheckedChange={field.handleChange} />
					</div>
				)}
			</form.Field>

			<form.Field name="applicationIds">
				{(field) => (
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
			</form.Field>
		</FieldGroup>
	);
}

function NotificationProviderStepContent({ form }: { form: NotificationFormApi }) {
	return (
		<form.Subscribe selector={(state) => state.values.type}>
			{(type) => {
				if (type === "discord") {
					return (
						<FieldGroup>
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
						</FieldGroup>
					);
				}

				if (type === "gotify") {
					return (
						<FieldGroup>
							<form.Field
								name="gotifyServerUrl"
								children={(field) => <GotifyServerUrlField field={field} />}
							/>

							<form.Field
								name="gotifyAppToken"
								children={(field) => <GotifyAppTokenField field={field} />}
							/>

							<form.Field
								name="gotifyPriority"
								children={(field) => <GotifyPriorityField field={field} />}
							/>

							<form.Field
								name="gotifyCustomPath"
								children={(field) => <GotifyCustomPathField field={field} />}
							/>
						</FieldGroup>
					);
				}

				if (type === "slack") {
					return (
						<FieldGroup>
							<form.Field
								name="slackWebhookUrl"
								children={(field) => <SlackWebhookUrlField field={field} />}
							/>
							<p className="text-sm text-muted-foreground">
								{m.notificationSlackAppSettingsNote()}
							</p>
						</FieldGroup>
					);
				}

				if (type === "webhook") {
					return (
						<FieldGroup>
							<form.Field
								name="webhookUrl"
								children={(field) => <WebhookUrlField field={field} />}
							/>

							<form.Field
								name="webhookHeaders"
								children={(field) => <WebhookHeadersField field={field} />}
							/>
						</FieldGroup>
					);
				}

				if (type === "custom") {
					return (
						<FieldGroup>
							<form.Field
								name="customShoutrrrUrl"
								children={(field) => <CustomShoutrrrUrlField field={field} />}
							/>
						</FieldGroup>
					);
				}

				return null;
			}}
		</form.Subscribe>
	);
}

function StepperNavigation({
	stepper,
	onNext,
	handleClose,
	isSubmitting,
}: {
	stepper: { state: { current: { index: number; data: { id: string } }; isLast: boolean } };
	onNext: (advance: () => void) => void;
	handleClose: () => void;
	isSubmitting: boolean;
}) {
	const isAtFirstVisibleStep = stepper.state.current.index === 0;

	return (
		<div className="flex items-center justify-between gap-4 pt-2">
			<Button type="button" variant="outline" disabled={isSubmitting} onClick={handleClose}>
				{m.cancel()}
			</Button>
			<div className="flex gap-2">
				{!isAtFirstVisibleStep && (
					<Stepper.Prev
						render={(domProps) => (
							<Button type="button" variant="outline" disabled={isSubmitting} {...domProps}>
								{m.previous()}
							</Button>
						)}
					/>
				)}
				{stepper.state.isLast ? (
					<Button type="submit" disabled={isSubmitting}>
						{isSubmitting ? m.savingDots() : m.addNotification()}
					</Button>
				) : (
					<Stepper.Next
						render={(domProps) => (
							<Button
								type="button"
								disabled={isSubmitting}
								onClick={(e) => onNext(() => domProps.onClick?.(e))}
							>
								{m.next()}
							</Button>
						)}
					/>
				)}
			</div>
		</div>
	);
}

export default function CreateNotificationDialog() {
	const [open, setOpen] = useState(false);
	const [isSubmitting, setIsSubmitting] = useState(false);

	const { data: applications } = useFetch<ApplicationListItem[]>("/applications");

	const form = useForm({
		defaultValues: {
			name: "",
			type: "discord" as NotificationType,
			discordWebhookUrl: "",
			discordBotName: "",
			discordAvatarUrl: "",
			discordThreadId: "",
			gotifyServerUrl: "",
			gotifyAppToken: "",
			gotifyPriority: "5",
			gotifyCustomPath: "",
			slackWebhookUrl: "",
			webhookUrl: "",
			webhookHeaders: "",
			customShoutrrrUrl: "",
			enabled: true,
			enableByDefault: false,
			applicationIds: [] as string[],
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

				await createNotification(payload);
				toast.success(m.notificationCreated());

				setOpen(false);
				form.reset();
			} catch (err) {
				toast.error(err instanceof Error ? err.message : m.failedSaveNotification());
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	const handleClose = () => {
		setOpen(false);
		form.reset();
	};

	async function handleNext(advance: () => void) {
		const [nameErrors, typeErrors] = await Promise.all([
			form.validateField("name", "submit"),
			form.validateField("type", "submit"),
		]);

		if (nameErrors?.length || typeErrors?.length) {
			return;
		}

		advance();
	}

	return (
		<Dialog
			open={open}
			onOpenChange={(next) => (next ? setOpen(true) : handleClose())}
			modal={false}
		>
			<DialogTrigger asChild>
				<Button>
					<Plus className="h-4 w-4" />
					{m.addNotification()}
				</Button>
			</DialogTrigger>

			<DialogContent className="sm:max-w-106.25">
				<DialogHeader>
					<DialogTitle>{m.addNotification()}</DialogTitle>
					<DialogDescription>{m.addNotificationDescription()}</DialogDescription>
				</DialogHeader>

				<form
					onSubmit={async (e) => {
						e.preventDefault();
						await form.handleSubmit();
					}}
				>
					<Stepper.Root key={String(open)} className="w-full space-y-6" orientation="horizontal">
						{({ stepper }) => {
							const allSteps = stepper.state.all;
							const currentIndex = stepper.state.current.index;

							return (
								<>
									<Stepper.List className="flex list-none gap-2 flex-row items-center justify-between">
										{allSteps.map((stepData, displayIndex) => {
											const status: StepStatus =
												displayIndex < currentIndex
													? "success"
													: displayIndex === currentIndex
														? "active"
														: "inactive";
											const isLast = displayIndex === allSteps.length - 1;

											return (
												<Fragment key={stepData.id}>
													<Stepper.Item
														step={stepData.id}
														className="group peer relative flex shrink-0 items-center gap-2"
													>
														<StepperTriggerWrapper displayNumber={displayIndex + 1} />
													</Stepper.Item>
													<StepperSeparatorWithStatus status={status} isLast={isLast} />
												</Fragment>
											);
										})}
									</Stepper.List>

									{stepper.flow.switch({
										config: () => (
											<Stepper.Content
												step="config"
												render={(props) => (
													<div {...props} className={cn("space-y-4", props.className)}>
														<NotificationConfigStepContent
															form={form}
															applications={applications}
														/>
													</div>
												)}
											/>
										),
										provider: () => (
											<Stepper.Content
												step="provider"
												render={(props) => (
													<div {...props} className={cn("space-y-4", props.className)}>
														<NotificationProviderStepContent form={form} />
													</div>
												)}
											/>
										),
									})}

									<StepperNavigation
										stepper={stepper}
										onNext={handleNext}
										handleClose={handleClose}
										isSubmitting={isSubmitting}
									/>
								</>
							);
						}}
					</Stepper.Root>
				</form>
			</DialogContent>
		</Dialog>
	);
}
