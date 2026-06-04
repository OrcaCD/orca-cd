import { fetcher } from "./api";

export type NotificationStatus = "unknown" | "success" | "error" | "healthy" | "unhealthy";
export const notificationTypes = ["discord", "slack", "webhook"] as const;
export type NotificationType = (typeof notificationTypes)[number];

export interface Notification {
	id: string;
	name: string;
	enabled: boolean;
	enableByDefault: boolean;
	status: NotificationStatus;
	type: NotificationType;
	applicationIds: string[];
	createdAt: string;
	updatedAt: string;
}

export interface UpsertNotificationRequest {
	name: string;
	enabled?: boolean;
	enableByDefault?: boolean;
	type: NotificationType;
	config: string;
	applicationIds?: string[];
}

export function isHttpUrl(rawUrl: string): boolean {
	try {
		const parsedUrl = new URL(rawUrl);
		return parsedUrl.protocol === "http:" || parsedUrl.protocol === "https:";
	} catch {
		return false;
	}
}

export function normalizeNotificationApplicationIds(ids: string[]): string[] {
	return Array.from(new Set(ids));
}

export function createNotification(data: UpsertNotificationRequest): Promise<Notification> {
	return fetcher<Notification>("/notifications", "POST", data);
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
