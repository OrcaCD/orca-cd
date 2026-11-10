import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { InfoTooltip, InfoTooltipContent, InfoTooltipTrigger } from "@/components/info-tooltip";
import { m } from "@/lib/paraglide/messages";
import { getLocale } from "@/lib/paraglide/runtime";
import { cn } from "@/lib/utils";
import { useApplicationActivity } from "./activity";

export function DeploymentActivityCard() {
	const { data } = useApplicationActivity();

	const succeeded = data?.days.reduce((sum, day) => sum + day.succeeded, 0) ?? 0;
	const failed = data?.days.reduce((sum, day) => sum + day.failed, 0) ?? 0;
	const total = succeeded + failed;
	const maxPerDay = Math.max(1, ...(data?.days.map((day) => day.succeeded + day.failed) ?? []));
	const dayFormatter = new Intl.DateTimeFormat(getLocale(), {
		weekday: "short",
		day: "numeric",
		month: "short",
	});

	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.applicationsActivityTitle()}</CardTitle>
				<CardDescription>{m.applicationsActivityLast7Days()}</CardDescription>
			</CardHeader>
			<CardContent className="space-y-3">
				{data ? (
					<>
						<div className="flex items-baseline gap-2">
							<span className="text-2xl font-bold">{total}</span>
							{total > 0 ? (
								<span
									className={cn("text-xs", failed > 0 ? "text-amber-400" : "text-muted-foreground")}
								>
									{m.applicationsActivitySuccessRate({
										rate: Math.round((succeeded / total) * 100),
									})}
								</span>
							) : null}
						</div>
						<div className="flex h-10 items-end gap-1">
							{data.days.map((day) => {
								const dayTotal = day.succeeded + day.failed;
								return (
									<InfoTooltip key={day.date}>
										<InfoTooltipTrigger
											aria-label={dayFormatter.format(new Date(`${day.date}T00:00`))}
											render={
												<div className="flex h-full flex-1 flex-col justify-end gap-px rounded-sm hover:bg-muted/50" />
											}
										>
											{dayTotal === 0 ? <div className="h-0.5 rounded-full bg-muted" /> : null}
											{day.failed > 0 ? (
												<div
													className="rounded-t-sm bg-red-500"
													style={{ height: `${(day.failed / maxPerDay) * 100}%` }}
												/>
											) : null}
											{day.succeeded > 0 ? (
												<div
													className={cn("bg-primary", day.failed === 0 && "rounded-t-sm")}
													style={{ height: `${(day.succeeded / maxPerDay) * 100}%` }}
												/>
											) : null}
										</InfoTooltipTrigger>
										<InfoTooltipContent>
											<p className="font-medium">
												{dayFormatter.format(new Date(`${day.date}T00:00`))}
											</p>
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
						<Skeleton className="h-10 w-full" />
					</>
				)}
			</CardContent>
		</Card>
	);
}
