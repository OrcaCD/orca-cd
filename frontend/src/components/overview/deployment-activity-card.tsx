import { Rocket } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { InfoTooltip, InfoTooltipContent, InfoTooltipTrigger } from "@/components/info-tooltip";
import { m } from "@/lib/paraglide/messages";
import { getLocale } from "@/lib/paraglide/runtime";
import { cn } from "@/lib/utils";
import { useApplicationActivity } from "./activity";
import { Metric, OverviewCardHeader } from "./overview-card";

export function DeploymentActivityCard() {
	const { data } = useApplicationActivity();

	const succeeded = data?.days.reduce((sum, day) => sum + day.succeeded, 0) ?? 0;
	const failed = data?.days.reduce((sum, day) => sum + day.failed, 0) ?? 0;
	const total = succeeded + failed;
	const maxPerDay = Math.max(1, ...(data?.days.map((day) => day.succeeded + day.failed) ?? []));
	const locale = getLocale();
	const dayFormatter = new Intl.DateTimeFormat(locale, {
		weekday: "short",
		day: "numeric",
		month: "short",
	});
	const weekdayFormatter = new Intl.DateTimeFormat(locale, { weekday: "short" });

	return (
		<Card>
			<OverviewCardHeader
				icon={Rocket}
				title={m.applicationsActivityTitle()}
				description={m.applicationsActivityLast7Days()}
			/>
			<CardContent className="flex flex-1 flex-col gap-4">
				{data ? (
					<>
						<Metric
							value={total}
							label={
								total > 0
									? m.applicationsActivitySuccessRate({
											rate: Math.round((succeeded / total) * 100),
										})
									: m.applicationsActivityNoDeployments()
							}
						/>
						<div className="mt-auto grid grid-cols-7 gap-1.5">
							{data.days.map((day, index) => {
								const dayTotal = day.succeeded + day.failed;
								const date = new Date(`${day.date}T00:00`);
								const isToday = index === data.days.length - 1;
								return (
									<InfoTooltip key={day.date}>
										<InfoTooltipTrigger
											aria-label={dayFormatter.format(date)}
											render={
												<div className="group/day flex flex-col items-stretch gap-1.5 rounded-sm outline-none" />
											}
										>
											<div className="flex h-14 flex-col justify-end gap-px overflow-hidden rounded-[3px] bg-muted/60 group-hover/day:bg-muted">
												{day.failed > 0 ? (
													<div
														className="bg-red-500"
														style={{ height: `${(day.failed / maxPerDay) * 100}%` }}
													/>
												) : null}
												{day.succeeded > 0 ? (
													<div
														className="bg-emerald-500"
														style={{ height: `${(day.succeeded / maxPerDay) * 100}%` }}
													/>
												) : null}
												{dayTotal === 0 ? <div className="h-px" /> : null}
											</div>
											<span
												className={cn(
													"text-center text-[0.6875rem] leading-none text-muted-foreground",
													isToday && "font-medium text-foreground",
												)}
											>
												{weekdayFormatter.format(date)}
											</span>
										</InfoTooltipTrigger>
										<InfoTooltipContent>
											<p className="font-medium">{dayFormatter.format(date)}</p>
											<p>
												{m.applicationsActivityDayTooltip({
													succeeded: day.succeeded,
													failed: day.failed,
												})}
											</p>
										</InfoTooltipContent>
									</InfoTooltip>
								);
							})}
						</div>
					</>
				) : (
					<>
						<Skeleton className="h-8 w-20" />
						<Skeleton className="mt-auto h-18 w-full" />
					</>
				)}
			</CardContent>
		</Card>
	);
}
