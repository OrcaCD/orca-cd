import { createFileRoute, Link } from "@tanstack/react-router";
import { useForm, useStore } from "@tanstack/react-form";
import { ArrowRight, Bell, Info, WebhookIcon } from "lucide-react";
import { useState } from "react";
import z from "zod";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { toast } from "@/components/ui/toast";
import { useFetch } from "@/lib/api";
import { updateRollbackSettings, type Application } from "@/lib/applications";
import {
	getNotificationTypeIconPath,
	type Notification,
	type NotificationEvent,
} from "@/lib/notifications";
import { m } from "@/lib/paraglide/messages";

export const Route = createFileRoute("/_authenticated/applications/$id/settings/rollback")({
	component: RollbackSettingsPage,
	head: () => ({
		meta: [
			{
				title: `${m.navApplications()} - ${m.rollbackSettingsTitle()}`,
			},
		],
	}),
});

const rollbackSchema = z.object({
	enabled: z.boolean(),
	onDeployFailure: z.boolean(),
	onUnhealthy: z.boolean(),
	healthTimeoutMinutes: z
		.number()
		.int(m.validationRollbackTimeout())
		.min(1, m.validationRollbackTimeout())
		.max(60, m.validationRollbackTimeout()),
	pauseAutoSync: z.boolean(),
});

const rollbackEvents: NotificationEvent[] = [
	"application.rollback.succeeded",
	"application.rollback.failed",
];

function notifiesAboutRollbacks(notification: Notification, applicationId: string): boolean {
	return (
		notification.enabled &&
		notification.events.some((event) => rollbackEvents.includes(event)) &&
		(notification.allApplications || notification.applicationIds.includes(applicationId))
	);
}

function RollbackSettingsPage() {
	const { id } = Route.useParams();
	const { data: application, mutate: mutateApplication } = useFetch<Application>(
		`/applications/${id}`,
	);
	const { data: notifications } = useFetch<Notification[]>("/notifications");
	const [isSubmitting, setIsSubmitting] = useState(false);

	const settings = application?.rollback;
	const form = useForm({
		defaultValues: {
			enabled: settings?.enabled ?? false,
			onDeployFailure: settings?.onDeployFailure ?? true,
			onUnhealthy: settings?.onUnhealthy ?? true,
			healthTimeoutMinutes: Math.round((settings?.healthTimeoutSeconds ?? 300) / 60),
			pauseAutoSync: settings?.pauseAutoSync ?? true,
		},
		validators: {
			onSubmit: rollbackSchema,
		},
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				const updated = await updateRollbackSettings(id, {
					enabled: value.enabled,
					onDeployFailure: value.onDeployFailure,
					onUnhealthy: value.onUnhealthy,
					healthTimeoutSeconds: value.healthTimeoutMinutes * 60,
					pauseAutoSync: value.pauseAutoSync,
				});
				// Update the cache first, otherwise the form falls back to the stale defaults after the reset
				await mutateApplication(updated, { revalidate: false });
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

	const enabled = useStore(form.store, (state) => state.values.enabled);
	const onUnhealthy = useStore(form.store, (state) => state.values.onUnhealthy);
	const subscribed = notifications?.filter((notification) =>
		notifiesAboutRollbacks(notification, id),
	);

	return (
		<div className="flex flex-col gap-6">
			<Card>
				<CardHeader>
					<CardTitle>{m.automaticRollback()}</CardTitle>
					<CardDescription>{m.automaticRollbackDescription()}</CardDescription>
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
							<form.Field name="enabled">
								{(field) => (
									<Field>
										<div className="flex items-center gap-3">
											<Switch
												id={field.name}
												checked={field.state.value}
												onCheckedChange={(checked) => field.handleChange(checked)}
											/>
											<Label htmlFor={field.name}>{m.automaticRollback()}</Label>
										</div>
									</Field>
								)}
							</form.Field>

							{enabled ? (
								<>
									<div className="space-y-1">
										<h3 className="font-medium">{m.rollbackWhen()}</h3>
										<p className="text-muted-foreground text-xs">{m.rollbackWhenDescription()}</p>
									</div>
									<form.Field name="onDeployFailure">
										{(field) => (
											<CheckboxField
												id={field.name}
												checked={field.state.value}
												onChange={field.handleChange}
												label={m.rollbackOnDeployFailure()}
												description={m.rollbackOnDeployFailureDescription()}
											/>
										)}
									</form.Field>
									<form.Field name="onUnhealthy">
										{(field) => (
											<CheckboxField
												id={field.name}
												checked={field.state.value}
												onChange={field.handleChange}
												label={m.rollbackOnUnhealthy()}
												description={m.rollbackOnUnhealthyDescription()}
											/>
										)}
									</form.Field>
									{onUnhealthy ? (
										<form.Field name="healthTimeoutMinutes">
											{(field) => {
												const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
												return (
													<Field data-invalid={isInvalid} className="pl-6">
														<div className="flex items-center gap-2">
															<Input
																id={field.name}
																type="number"
																min={1}
																max={60}
																className="w-24"
																aria-label={m.rollbackOnUnhealthy()}
																value={field.state.value}
																onBlur={field.handleBlur}
																onChange={(e) => field.handleChange(Number(e.target.value))}
															/>
															<span className="text-sm text-muted-foreground">
																{m.rollbackMinutes()}
															</span>
														</div>
														{isInvalid && <FieldError errors={field.state.meta.errors} />}
													</Field>
												);
											}}
										</form.Field>
									) : null}

									<div className="space-y-1">
										<h3 className="font-medium">{m.rollbackAfter()}</h3>
										<p className="text-muted-foreground text-xs">{m.rollbackAfterDescription()}</p>
									</div>
									<form.Field name="pauseAutoSync">
										{(field) => (
											<CheckboxField
												id={field.name}
												checked={field.state.value}
												onChange={field.handleChange}
												label={m.rollbackPauseAutoSync()}
												description={m.rollbackPauseAutoSyncDescription()}
											/>
										)}
									</form.Field>
								</>
							) : null}

							<p className="flex items-center gap-2 text-xs text-muted-foreground">
								<Info className="size-3.5 shrink-0" />
								{m.rollbackVolumesNote()}
							</p>

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

			<Card>
				<CardHeader>
					<CardTitle className="flex items-center gap-2">
						<Bell className="h-5 w-5" />
						{m.rollbackNotify()}
					</CardTitle>
					<CardDescription>{m.rollbackNotifyDescription()}</CardDescription>
				</CardHeader>
				<Separator />
				<CardContent className="space-y-4">
					{subscribed && subscribed.length > 0 ? (
						<ul className="flex flex-wrap gap-2">
							{subscribed.map((notification) => (
								<li
									key={notification.id}
									className="flex items-center gap-2 rounded-md bg-muted px-3 py-1.5 text-sm"
								>
									{notification.type === "webhook" ? (
										<WebhookIcon className="size-4" />
									) : (
										<img
											src={getNotificationTypeIconPath(notification.type)}
											alt={m.notificationProviderAlt()}
											className="size-4"
										/>
									)}
									{notification.name}
								</li>
							))}
						</ul>
					) : subscribed ? (
						<p className="text-sm text-muted-foreground">{m.rollbackNotifyEmpty()}</p>
					) : null}
					<Button
						variant="outline"
						size="sm"
						nativeButton={false}
						render={<Link to="/notifications" />}
					>
						{m.manageNotifications()}
						<ArrowRight />
					</Button>
				</CardContent>
			</Card>
		</div>
	);
}

function CheckboxField({
	id,
	checked,
	onChange,
	label,
	description,
}: {
	id: string;
	checked: boolean;
	onChange: (checked: boolean) => void;
	label: string;
	description: string;
}) {
	return (
		<Field>
			<div className="flex items-start gap-2">
				<Checkbox id={id} checked={checked} onCheckedChange={(value) => onChange(value === true)} />
				<div className="space-y-1">
					<Label htmlFor={id}>{label}</Label>
					<p className="text-muted-foreground text-xs">{description}</p>
				</div>
			</div>
		</Field>
	);
}
