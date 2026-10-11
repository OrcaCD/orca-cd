// oxlint-disable react/no-children-prop
import { useForm } from "@tanstack/react-form";
import { Pencil } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { z } from "zod";
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
import { FieldGroup } from "@/components/ui/field";
import { Switch } from "@/components/ui/switch";
import {
	normalizeNotificationSubscription,
	type Notification,
	updateNotification,
} from "@/lib/notifications";
import { m } from "@/lib/paraglide/messages";
import {
	NotificationSubscriptionFields,
	notificationSubscriptionSchema,
} from "./notification-subscription-fields";

const notificationSettingsSchema = z.object({
	enabled: z.boolean(),
	subscription: notificationSubscriptionSchema,
});

export default function UpdateNotificationDialog({
	notification,
	asDropdownItem = false,
}: {
	notification: Notification;
	asDropdownItem?: boolean;
}) {
	const [open, setOpen] = useState(false);
	const [isLoading, setIsLoading] = useState(false);

	const form = useForm({
		defaultValues: {
			enabled: notification.enabled,
			subscription: {
				events: notification.events,
				allApplications: notification.allApplications,
				applicationIds: notification.applicationIds,
				allAgents: notification.allAgents,
				agentIds: notification.agentIds,
				allRepositories: notification.allRepositories,
				repositoryIds: notification.repositoryIds,
			},
		},
		validators: { onSubmit: notificationSettingsSchema },
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			try {
				await updateNotification(notification.id, {
					enabled: value.enabled,
					...normalizeNotificationSubscription(value.subscription),
				});
				toast.success(m.notificationUpdated());
				setOpen(false);
				form.reset();
			} catch (err) {
				toast.error(err instanceof Error ? err.message : m.failedSaveNotification());
			} finally {
				setIsLoading(false);
			}
		},
	});

	const handleOpen = () => {
		form.reset();
		setOpen(true);
	};

	const handleClose = () => {
		setOpen(false);
		form.reset();
	};

	return (
		<Dialog open={open} onOpenChange={(next) => (next ? handleOpen() : handleClose())}>
			<DialogTrigger
				nativeButton={!asDropdownItem}
				render={
					asDropdownItem ? (
						<DropdownMenuItem onSelect={(event) => event.preventDefault()}>
							<Pencil className="h-4 w-4" />
							{m.edit()}
						</DropdownMenuItem>
					) : (
						<Button variant="ghost" size="icon">
							<Pencil className="h-4 w-4" />
							<span className="sr-only">{m.editNotification()}</span>
						</Button>
					)
				}
			></DialogTrigger>
			<DialogContent className="sm:max-w-lg">
				<DialogHeader>
					<DialogTitle>{m.editNotification()}</DialogTitle>
					<DialogDescription>{m.editNotificationDescription()}</DialogDescription>
				</DialogHeader>
				<form
					onSubmit={async (event) => {
						event.preventDefault();
						await form.handleSubmit();
					}}
				>
					<FieldGroup>
						<form.Field
							name="enabled"
							children={(field) => (
								<div className="flex items-center justify-between gap-4 rounded-md border p-3">
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
							name="subscription"
							validators={{ onSubmit: notificationSubscriptionSchema }}
							children={(field) => (
								<NotificationSubscriptionFields
									value={field.state.value}
									onChange={field.handleChange}
									errors={field.state.meta.errors}
									modal={false}
								/>
							)}
						/>

						<div className="flex flex-wrap gap-2 pt-2">
							<Button type="submit" disabled={isLoading}>
								{isLoading ? m.savingDots() : m.saveChanges()}
							</Button>
							<Button type="button" variant="outline" onClick={handleClose} disabled={isLoading}>
								{m.cancel()}
							</Button>
						</div>
					</FieldGroup>
				</form>
			</DialogContent>
		</Dialog>
	);
}
