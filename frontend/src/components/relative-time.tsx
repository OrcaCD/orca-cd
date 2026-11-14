import { useMemo, type ReactNode } from "react";
import { z } from "zod";
import { InfoTooltip, InfoTooltipContent, InfoTooltipTrigger } from "@/components/info-tooltip";
import { useNow } from "@/hooks/use-now";
import { m } from "@/lib/paraglide/messages";
import { getLocale } from "@/lib/paraglide/runtime";
import { cn, formatRelativeTime } from "@/lib/utils";

const timestampSchema = z.iso.datetime({ offset: true });

export function RelativeTime({
	value,
	className,
	fallback,
	format,
}: {
	value?: string | Date | null;
	className?: string;
	fallback?: ReactNode;
	format?: (relative: string) => string;
}) {
	const now = useNow();
	// Parsed once, as the clock renders every timestamp again each minute.
	const date = useMemo(
		() =>
			value instanceof Date
				? value
				: typeof value === "string" && timestampSchema.safeParse(value).success
					? new Date(value)
					: null,
		[value],
	);
	if (!date || Number.isNaN(date.getTime())) {
		return <span className={className}>{fallback ?? m.never()}</span>;
	}

	const relative = formatRelativeTime(date, Math.max(now, date.getTime()));
	return (
		<InfoTooltip>
			<InfoTooltipTrigger
				render={<time dateTime={date.toISOString()} />}
				nativeButton={false}
				className={cn("cursor-help whitespace-nowrap", className)}
			>
				{format ? format(relative) : relative}
			</InfoTooltipTrigger>
			<InfoTooltipContent>
				<LocaleDateTime date={date} />
			</InfoTooltipContent>
		</InfoTooltip>
	);
}

// A component, so the date is only formatted when the tooltip is shown.
export function LocaleDateTime({ date }: { date: Date }) {
	return date.toLocaleString(getLocale());
}
