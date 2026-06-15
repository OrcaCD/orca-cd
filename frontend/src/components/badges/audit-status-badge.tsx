import { Badge } from "@/components/ui/badge";

export function EventTypeBadge({ type }: { type: string }) {
	const badgeVariant = getBadgeVariant(type);

	return (
		<div className="flex items-center gap-2">
			<Badge variant={badgeVariant}>{type.toUpperCase()}</Badge>
		</div>
	);
}

function getBadgeVariant(type: string): Parameters<typeof Badge>[0]["variant"] {
	switch (type) {
		case "created":
		case "enabled-image-webhook":
			return "success";
		case "deleted":
		case "rotated-token":
		case "disabled-image-webhook":
			return "destructive";
		default:
			return "secondary";
	}
}
