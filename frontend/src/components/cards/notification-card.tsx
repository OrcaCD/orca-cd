import { useState, type ReactNode } from "react";
import { mutate } from "swr";
import { AppWindow, Check, GitBranch, Minus, Server, WebhookIcon } from "lucide-react";
import { getNotificationStatusLabel } from "@/components/badges/notification-status-badge";
import {
	notificationEventLabels,
	resourceConfigs,
} from "@/components/dialogs/notification-subscription-fields";
import { Switch } from "@/components/ui/switch";
import { toast } from "@/components/ui/toast";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { API_BASE } from "@/lib/api";
import {
	getNotificationResourceEvents,
	getNotificationTypeIconPath,
	normalizeNotificationSubscription,
	notificationResources,
	updateNotification,
	type Notification,
	type NotificationResource,
	type NotificationStatus,
} from "@/lib/notifications";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";
import { ResourceCard } from "./resource-card";

const resourceIcons: Record<NotificationResource, typeof AppWindow> = {
	applications: AppWindow,
	agents: Server,
	repositories: GitBranch,
};

const selectedScopeLabels: Record<NotificationResource, (count: number) => string> = {
	applications: (count) => m.cardScopeApplications({ count }),
	agents: (count) => m.cardScopeAgents({ count }),
	repositories: (count) => m.cardScopeRepositories({ count }),
};

const deliveryDotClass: Record<NotificationStatus, string> = {
	success: "bg-emerald-500",
	healthy: "bg-emerald-500",
	unhealthy: "bg-amber-500",
	error: "bg-red-500",
	unknown: "bg-zinc-400",
};

// One dot per event of the resource, filled when the notification is subscribed to it.
function SubscriptionRow({
	notification,
	resource,
}: {
	notification: Notification;
	resource: NotificationResource;
}) {
	const config = resourceConfigs[resource];
	const Icon = resourceIcons[resource];
	const events = getNotificationResourceEvents(resource);
	const subscribed = events.filter((event) => notification.events.includes(event));
	const scope = notification[config.allKey]
		? config.all()
		: selectedScopeLabels[resource](notification[config.idsKey].length);

	return (
		<li className="flex items-center gap-2.5">
			<Icon className="size-4 shrink-0 text-muted-foreground" />
			<span
				className={cn(
					"min-w-0 flex-1 truncate",
					subscribed.length === 0 && "text-muted-foreground",
				)}
			>
				{scope}
			</span>
			<Tooltip>
				<TooltipTrigger
					render={
						<span
							tabIndex={0}
							className="flex shrink-0 cursor-help items-center gap-1 rounded-sm p-1 outline-none focus-visible:ring-2 focus-visible:ring-ring"
							aria-label={m.cardSubscribedEvents({
								count: subscribed.length,
								total: events.length,
							})}
						/>
					}
				>
					<span className="mr-1 text-xs text-muted-foreground tabular-nums">
						{subscribed.length}/{events.length}
					</span>
					{events.map((event) => (
						<span
							key={event}
							className={cn(
								"size-2 rounded-full",
								subscribed.includes(event) ? "bg-primary" : "border border-foreground/25",
							)}
						/>
					))}
				</TooltipTrigger>
				<TooltipContent>
					<ul className="space-y-0.5">
						{events.map((event) => (
							<li
								key={event}
								className={cn(
									"flex items-center gap-1.5",
									!subscribed.includes(event) && "opacity-60",
								)}
							>
								{subscribed.includes(event) ? (
									<Check className="size-3" />
								) : (
									<Minus className="size-3" />
								)}
								{notificationEventLabels[event]()}
							</li>
						))}
					</ul>
				</TooltipContent>
			</Tooltip>
		</li>
	);
}

function EnabledSwitch({ notification }: { notification: Notification }) {
	const [pending, setPending] = useState<boolean | null>(null);
	const enabled = pending ?? notification.enabled;

	const handleChange = async (checked: boolean) => {
		setPending(checked);
		try {
			await updateNotification(notification.id, {
				enabled: checked,
				...normalizeNotificationSubscription({
					events: notification.events,
					allApplications: notification.allApplications,
					applicationIds: notification.applicationIds,
					allAgents: notification.allAgents,
					agentIds: notification.agentIds,
					allRepositories: notification.allRepositories,
					repositoryIds: notification.repositoryIds,
				}),
			});
			await mutate(`${API_BASE}/notifications`);
			toast.add({ type: "success", title: m.notificationUpdated() });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedSaveNotification(),
			});
		} finally {
			setPending(null);
		}
	};

	return (
		<label className="flex shrink-0 cursor-pointer items-center gap-2">
			<span>{enabled ? m.enabled() : m.disabled()}</span>
			<Switch
				size="sm"
				checked={enabled}
				disabled={pending !== null}
				onCheckedChange={(checked) => void handleChange(checked)}
			/>
		</label>
	);
}

export function NotificationCard({
	notification,
	action,
}: {
	notification: Notification;
	action?: ReactNode;
}) {
	return (
		<ResourceCard
			icon={
				notification.type === "webhook" ? (
					<WebhookIcon className="size-6" />
				) : (
					<img
						src={getNotificationTypeIconPath(notification.type)}
						alt={m.notificationProviderAlt()}
						className="size-6"
					/>
				)
			}
			title={notification.name}
			subtitle={notification.type.charAt(0).toUpperCase() + notification.type.slice(1)}
			action={action}
			footer={
				<>
					<span className="flex min-w-0 items-center gap-1.5">
						<span
							className={cn("size-2 shrink-0 rounded-full", deliveryDotClass[notification.status])}
						/>
						<span className="truncate">{m.cardLastDelivery()}</span>
						<span className="font-medium text-foreground">
							{getNotificationStatusLabel(notification.status)}
						</span>
					</span>
					<EnabledSwitch notification={notification} />
				</>
			}
		>
			<ul className={cn("space-y-2 transition-opacity", !notification.enabled && "opacity-50")}>
				{notificationResources.map((resource) => (
					<SubscriptionRow key={resource} notification={notification} resource={resource} />
				))}
			</ul>
		</ResourceCard>
	);
}
