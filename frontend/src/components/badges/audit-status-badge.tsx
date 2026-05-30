const eventTypeStyles: Record<string, string> = {
	created: "text-green-600 bg-green-50 dark:bg-green-950/30",
	updated: "text-blue-600 bg-blue-50 dark:bg-blue-950/30",
	deleted: "text-red-600 bg-red-50 dark:bg-red-950/30",
};

export function EventTypeBadge({ type }: { type: string }) {
	const className =
		eventTypeStyles[type] ?? "text-gray-600 bg-gray-50";

	return (
		<span
			className={`px-2 py-0.5 rounded text-xs font-semibold uppercase tracking-wider ${className}`}
		>
			{type}
		</span>
	);
}
