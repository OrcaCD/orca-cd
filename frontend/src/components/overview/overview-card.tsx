import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

/** Header shared by the overview cards: icon, title and a one line description. */
export function OverviewCardHeader({
	icon: Icon,
	title,
	description,
}: {
	icon: LucideIcon;
	title: ReactNode;
	description?: ReactNode;
}) {
	return (
		<CardHeader className="gap-0.5">
			<CardTitle className="flex items-center gap-2 text-sm font-medium">
				<Icon className="size-4 text-muted-foreground" />
				{title}
			</CardTitle>
			{description ? <CardDescription className="text-xs">{description}</CardDescription> : null}
		</CardHeader>
	);
}

export type MetricTone = "default" | "ok" | "warning" | "error";

const metricToneClass: Record<MetricTone, string> = {
	default: "",
	ok: "text-emerald-700 dark:text-emerald-400",
	warning: "text-amber-700 dark:text-amber-400",
	error: "text-red-600 dark:text-red-400",
};

/** Headline number of an overview card with its unit, e.g. "1 / 2 online". */
export function Metric({
	value,
	label,
	tone = "default",
	className,
}: {
	value: ReactNode;
	label: ReactNode;
	tone?: MetricTone;
	className?: string;
}) {
	return (
		<div className={cn("flex items-baseline gap-2", className)}>
			<span
				className={cn(
					"text-3xl leading-none font-semibold tracking-tight tabular-nums",
					metricToneClass[tone],
				)}
			>
				{value}
			</span>
			<span className="text-sm text-muted-foreground">{label}</span>
		</div>
	);
}
