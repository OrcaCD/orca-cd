import { Badge } from "../ui/badge";

export function EventTypeBadge({ type }: { type: string }) {
	const badgeVariant =
		type === "created" ? "success" : type === "deleted" ? "destructive" : "secondary";

	return (
		<div className="flex items-center gap-2">
			<Badge variant={badgeVariant}>{type.toUpperCase()}</Badge>
		</div>
	);
}
