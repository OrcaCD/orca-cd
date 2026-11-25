import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import {
	Card,
	CardAction,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";

export function StatCard({
	icon: Icon,
	title,
	value,
	chart,
	children,
}: {
	icon: LucideIcon;
	title: string;
	value: string;
	chart?: ReactNode;
	children?: ReactNode;
}) {
	return (
		<Card>
			<CardHeader>
				<CardDescription>{title}</CardDescription>
				<CardTitle className="text-2xl font-semibold tabular-nums">{value}</CardTitle>
				<CardAction className="text-muted-foreground">
					<Icon className="size-5" />
				</CardAction>
			</CardHeader>
			{chart || children ? (
				// Two lines high, so a line that shows up later doesn't move the card
				<CardContent className="flex min-h-9 flex-col gap-0.5 text-xs text-muted-foreground">
					{chart ? <div className="mb-1.5">{chart}</div> : null}
					{children}
				</CardContent>
			) : null}
		</Card>
	);
}
