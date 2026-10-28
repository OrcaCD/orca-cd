import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
	formatHealthDuration,
	healthLegend,
	healthStatusConfig,
	type HealthBucket,
	type HealthStats,
} from "@/lib/application-health";
import { HealthStatus } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { getLocale } from "@/lib/paraglide/runtime";
import { cn } from "@/lib/utils";

const bucketBreakdown: { status: HealthStatus; seconds: (bucket: HealthBucket) => number }[] = [
	{ status: HealthStatus.Healthy, seconds: (bucket) => bucket.healthySeconds },
	{ status: HealthStatus.Degraded, seconds: (bucket) => bucket.degradedSeconds },
	{ status: HealthStatus.Unhealthy, seconds: (bucket) => bucket.unhealthySeconds },
	{ status: HealthStatus.Stopped, seconds: (bucket) => bucket.stoppedSeconds },
	{ status: HealthStatus.Unknown, seconds: (bucket) => bucket.noDataSeconds },
];

export function AvailabilityBar({ stats }: { stats: HealthStats }) {
	const locale = getLocale();
	const short = stats.range === "24h";
	const axisFormatter = new Intl.DateTimeFormat(
		locale,
		short ? { hour: "2-digit", minute: "2-digit" } : { day: "numeric", month: "short" },
	);
	const tooltipFormatter = new Intl.DateTimeFormat(locale, {
		day: "numeric",
		month: "short",
		hour: "2-digit",
		minute: "2-digit",
	});
	const middle = stats.buckets[Math.floor(stats.buckets.length / 2)];

	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.healthAvailability()}</CardTitle>
				<CardAction className="text-sm text-muted-foreground">
					{m.healthBarDuration({ duration: formatHealthDuration(stats.bucketSeconds) })}
				</CardAction>
			</CardHeader>
			<CardContent className="space-y-3">
				<div className="flex h-12 gap-0.5 sm:gap-1" role="img" aria-label={m.healthAvailability()}>
					{stats.buckets.map((bucket) => (
						<Tooltip key={bucket.start}>
							<TooltipTrigger
								render={
									<div
										className={cn(
											"h-full min-w-0 flex-1 rounded-[3px] transition-opacity hover:opacity-80",
											healthStatusConfig[bucket.status].className,
										)}
									/>
								}
							/>
							<TooltipContent className="flex-col items-start gap-1">
								<p className="font-medium">
									{tooltipFormatter.formatRange(new Date(bucket.start), new Date(bucket.end))}
								</p>
								{bucketBreakdown
									.filter((entry) => entry.seconds(bucket) > 0)
									.map((entry) => (
										<p key={entry.status} className="flex items-center gap-1.5">
											<span
												className={cn(
													"size-2 rounded-xs",
													healthStatusConfig[entry.status].className,
												)}
											/>
											{healthStatusConfig[entry.status].label()}:{" "}
											{formatHealthDuration(entry.seconds(bucket))}
										</p>
									))}
							</TooltipContent>
						</Tooltip>
					))}
				</div>
				<div className="flex justify-between text-xs text-muted-foreground">
					<span>{axisFormatter.format(new Date(stats.from))}</span>
					{middle ? <span>{axisFormatter.format(new Date(middle.start))}</span> : null}
					<span>{m.healthNow()}</span>
				</div>
				<div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 pt-1 text-xs text-muted-foreground">
					<div className="flex flex-wrap items-center gap-x-4 gap-y-2">
						{healthLegend.map((status) => (
							<span key={status} className="flex items-center gap-1.5">
								<span className={cn("size-2.5 rounded-xs", healthStatusConfig[status].className)} />
								{status === HealthStatus.Unknown
									? m.healthLegendNoData()
									: healthStatusConfig[status].label()}
							</span>
						))}
					</div>
					<span>{m.healthNoDataNotCounted()}</span>
				</div>
			</CardContent>
		</Card>
	);
}
