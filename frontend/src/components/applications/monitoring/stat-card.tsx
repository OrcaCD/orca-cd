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
	title,
	value,
	aside,
	chart,
	children,
}: {
	title: string;
	value: string;
	aside?: ReactNode;
	chart?: ReactNode;
	children?: ReactNode;
}) {
	return (
		<Card>
			<CardHeader>
				<CardDescription>{title}</CardDescription>
				<CardTitle className="text-2xl font-semibold tabular-nums">{value}</CardTitle>
				{aside ? (
					<CardAction className="text-right text-xs text-muted-foreground tabular-nums">
						{aside}
					</CardAction>
				) : null}
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
