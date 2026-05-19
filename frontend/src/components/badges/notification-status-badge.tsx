import type { NotificationStatus } from "@/lib/notifications";
import { m } from "@/lib/paraglide/messages";
import { Badge } from "../ui/badge";

function getStatusLabel(status: NotificationStatus): string {
	switch (status) {
		case "success":
			return m.successAlertTitle();
		case "error":
			return m.statusError();
		case "healthy":
			return m.statusHealthy();
		case "unhealthy":
			return m.statusUnhealthy();
		default:
			return m.unknown();
	}
}

function getStatusDotClass(status: NotificationStatus): string {
	switch (status) {
		case "success":
			return "bg-emerald-500";
		case "error":
			return "bg-red-500";
		case "healthy":
			return "bg-emerald-500";
		case "unhealthy":
			return "bg-amber-500";
		default:
			return "bg-zinc-400";
	}
}

export function NotificationStatusBadge({ status }: { status: NotificationStatus }) {
	const badgeVariant =
		status === "success" || status === "healthy"
			? "success"
			: status === "error"
				? "destructive"
				: "secondary";

	return (
		<div className="flex items-center gap-2">
			<Badge variant={badgeVariant}>
				<span className={`h-2 w-2 rounded-full ${getStatusDotClass(status)}`} aria-hidden="true" />
				{getStatusLabel(status)}
			</Badge>
		</div>
	);
}
