import type { ReactNode } from "react";
import { Bell, WebhookIcon } from "lucide-react";
import { getNotificationStatusLabel } from "@/components/badges/notification-status-badge";
import {
	getNotificationTypeIconPath,
	type Notification,
	type NotificationStatus,
} from "@/lib/notifications";
import { m } from "@/lib/paraglide/messages";
import { ResourceCard, type FlowState } from "./resource-card";

const deliveryState: Record<NotificationStatus, FlowState> = {
	success: "ok",
	healthy: "ok",
	unhealthy: "warning",
	error: "error",
	unknown: "unknown",
};

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
			stages={[
				{
					label: m.cardStageApplications(),
					value: notification.allApplications
						? m.allApplications()
						: notification.applicationIds.length,
					state:
						notification.allApplications || notification.applicationIds.length ? "info" : "off",
				},
				{
					label: m.cardStageChannel(),
					value: notification.enabled ? m.enabled() : m.disabled(),
					state: notification.enabled ? "ok" : "off",
				},
				{
					label: m.cardStageDelivery(),
					value: getNotificationStatusLabel(notification.status),
					state: deliveryState[notification.status],
				},
			]}
			meta={
				<>
					<Bell className="size-3.5 shrink-0" />
					<span className="truncate">
						{m.cardEventsCount({ count: notification.events.length })}
					</span>
				</>
			}
			time={notification.updatedAt}
		/>
	);
}
