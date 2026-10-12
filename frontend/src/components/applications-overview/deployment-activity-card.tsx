import { Link } from "@tanstack/react-router";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useFetch } from "@/lib/api";
import { ApplicationEventStatus } from "@/lib/application-events";
import type { ApplicationActivity, ApplicationActivityDeployment } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { getLocale } from "@/lib/paraglide/runtime";
import { cn, formatRelativeTime } from "@/lib/utils";

// Sent so the backend buckets deployments into the user's calendar days.
const utcOffset = -new Date().getTimezoneOffset();

export function DeploymentActivityCard() {
	const { data } = useFetch<ApplicationActivity>(`/applications/activity?utcOffset=${utcOffset}`);

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
									<Tooltip key={day.date}>
										<TooltipTrigger
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
										</TooltipTrigger>
										<TooltipContent>
											<p className="font-medium">
												{dayFormatter.format(new Date(`${day.date}T00:00`))}
											</p>
											<p>
												{m.applicationsActivityDayTooltip({
													succeeded: day.succeeded,
													failed: day.failed,
												})}
											</p>
										</TooltipContent>
									</Tooltip>
								);
							})}
						</div>
						<LastDeployment deployment={data.lastDeployment} />
					</>
				) : (
					<>
						<Skeleton className="h-8 w-20" />
						<Skeleton className="h-10 w-full" />
						<Skeleton className="h-4 w-3/4" />
					</>
				)}
			</CardContent>
		</Card>
	);
}

const lastDeploymentStatusClass: Partial<Record<ApplicationEventStatus, string>> = {
	[ApplicationEventStatus.Running]: "bg-blue-400 animate-pulse",
	[ApplicationEventStatus.Succeeded]: "bg-emerald-400",
	[ApplicationEventStatus.Failed]: "bg-red-400",
};

function LastDeployment({ deployment }: { deployment: ApplicationActivityDeployment | null }) {
	if (!deployment) {
		return <p className="text-xs text-muted-foreground">{m.applicationsActivityNoDeployments()}</p>;
	}

	const details = [
		formatRelativeTime(deployment.createdAt),
		deployment.commitHash?.slice(0, 7),
		deployment.actorName,
	].filter(Boolean);

	return (
		<Link
			to="/applications/$id"
			params={{ id: deployment.applicationId }}
			className="flex items-center gap-2 text-xs text-muted-foreground hover:text-foreground"
		>
			<span
				className={cn(
					"size-1.5 shrink-0 rounded-full",
					lastDeploymentStatusClass[deployment.status] ?? "bg-zinc-400",
				)}
			/>
			<StaticLucideIcon name={deployment.applicationIcon} className="size-3.5 shrink-0" />
			<span className="truncate">
				<span className="font-medium text-foreground">{deployment.applicationName}</span>
				{" · "}
				{details.join(" · ")}
			</span>
		</Link>
	);
}
