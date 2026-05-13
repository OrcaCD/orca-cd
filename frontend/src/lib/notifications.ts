import { fetcher } from "./api";

export type NotificationStatus = "unknown" | "success" | "error" | "healthy" | "unhealthy";
export type NotificationType = "discord";

export interface Notification {
	id: string;
	name: string;
	enabled: boolean;
	enableByDefault: boolean;
	status: NotificationStatus;
	type: NotificationType;
	config?: string;
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

export function createNotification(data: UpsertNotificationRequest): Promise<Notification> {
	return fetcher<Notification>("/notifications", "POST", data);
}

export function updateNotification(
	id: string,
	data: UpsertNotificationRequest,
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
