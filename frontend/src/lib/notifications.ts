import { fetcher } from "./api";

export type NotificationStatus = "unknown" | "success" | "error" | "healthy" | "unhealthy";
export const notificationTypes = [
	"discord",
	"gotify",
	"slack",
	"teams",
	"email",
	"webhook",
	"custom",
] as const;
export type NotificationType = (typeof notificationTypes)[number];

export const notificationEvents = [
	"application.deployment.succeeded",
	"application.deployment.failed",
	"application.image_update.succeeded",
	"application.image_update.failed",
	"application.sync.failed",
	"application.health.unhealthy",
	"application.health.recovered",
	"agent.offline",
	"agent.online",
	"repository.sync.failed",
	"repository.sync.recovered",
] as const;
export type NotificationEvent = (typeof notificationEvents)[number];

export const notificationResources = ["applications", "agents", "repositories"] as const;
export type NotificationResource = (typeof notificationResources)[number];

const notificationEventPrefixes: Record<NotificationResource, string> = {
	applications: "application.",
	agents: "agent.",
	repositories: "repository.",
};

export function getNotificationResourceEvents(resource: NotificationResource): NotificationEvent[] {
	return notificationEvents.filter((event) =>
		event.startsWith(notificationEventPrefixes[resource]),
	);
}

export interface NotificationSubscription {
	events: NotificationEvent[];
	allApplications: boolean;
	applicationIds: string[];
	allAgents: boolean;
	agentIds: string[];
	allRepositories: boolean;
	repositoryIds: string[];
}

export interface Notification extends NotificationSubscription {
	id: string;
	name: string;
	enabled: boolean;
	status: NotificationStatus;
	type: NotificationType;
	createdAt: string;
	updatedAt: string;
}

export interface UpsertNotificationRequest extends Partial<NotificationSubscription> {
	name: string;
	enabled?: boolean;
	type: NotificationType;
	config: string;
}

export interface UpdateNotificationRequest extends NotificationSubscription {
	enabled: boolean;
}

export function isHttpUrl(rawUrl: string): boolean {
	try {
		const parsedUrl = new URL(rawUrl);
		return parsedUrl.protocol === "http:" || parsedUrl.protocol === "https:";
	} catch {
		return false;
	}
}

export function normalizeNotificationSubscription(
	subscription: NotificationSubscription,
): NotificationSubscription {
	// Explicit IDs are redundant when a scope covers every resource.
	const scopedIds = (all: boolean, ids: string[]) => (all ? [] : Array.from(new Set(ids)));
	return {
		...subscription,
		applicationIds: scopedIds(subscription.allApplications, subscription.applicationIds),
		agentIds: scopedIds(subscription.allAgents, subscription.agentIds),
		repositoryIds: scopedIds(subscription.allRepositories, subscription.repositoryIds),
	};
}

export function createNotification(data: UpsertNotificationRequest): Promise<Notification> {
	return fetcher<Notification>("/notifications", "POST", data);
}

export function updateNotification(
	id: string,
	data: UpdateNotificationRequest,
): Promise<Notification> {
	return fetcher<Notification>(`/notifications/${id}`, "PUT", data);
}

export function deleteNotification(id: string): Promise<void> {
	return fetcher(`/notifications/${id}`, "DELETE");
}

export function testNotification(id: string, message?: string): Promise<{ message: string }> {
	const trimmedMessage = message?.trim();
	if (trimmedMessage) {
		return fetcher<{ message: string }>(`/notifications/${id}/test`, "POST", {
			message: trimmedMessage,
		});
	}

	return fetcher<{ message: string }>(`/notifications/${id}/test`, "POST");
}

export function getNotificationTypeIconPath(type: NotificationType): string {
	if (type === "custom") {
		return "/assets/icons/notifications/shoutrrr.svg";
	}

	return `/assets/icons/notifications/${type}.svg`;
}
