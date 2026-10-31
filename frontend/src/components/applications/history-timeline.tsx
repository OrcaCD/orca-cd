import { InfoTooltip, InfoTooltipContent, InfoTooltipTrigger } from "@/components/info-tooltip";
import { ApplicationEventStatusBadge } from "@/components/badges/application-event-status-badge";
import {
	ApplicationEventStatus,
	eventSourceLabel,
	eventTypeLabel,
	type ApplicationEvent,
} from "@/lib/application-events";
import { getLocale } from "@/lib/paraglide/runtime";
import { cn, formatDuration } from "@/lib/utils";

const dotClasses: Record<ApplicationEventStatus, string> = {
	[ApplicationEventStatus.Running]: "bg-primary motion-safe:animate-pulse",
	[ApplicationEventStatus.Succeeded]: "bg-emerald-500",
	[ApplicationEventStatus.Failed]: "bg-red-500",
	[ApplicationEventStatus.NoChange]: "bg-muted-foreground/40",
};

const dayInMs = 24 * 60 * 60 * 1000;

function startOfDay(date: Date): number {
	return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
}

function formatDay(date: Date): string {
	const now = new Date();
	const daysAgo = Math.round((startOfDay(now) - startOfDay(date)) / dayInMs);
	if (daysAgo === 0 || daysAgo === 1) {
		const label = new Intl.RelativeTimeFormat(getLocale(), { numeric: "auto" }).format(
			-daysAgo,
			"day",
		);
		return label.charAt(0).toUpperCase() + label.slice(1);
	}

	return new Intl.DateTimeFormat(getLocale(), {
		weekday: "long",
		day: "numeric",
		month: "long",
		year: date.getFullYear() === now.getFullYear() ? undefined : "numeric",
	}).format(date);
}

function groupByDay(events: ApplicationEvent[]) {
	const groups: { day: number; date: Date; events: ApplicationEvent[] }[] = [];
	for (const event of events) {
		const date = new Date(event.createdAt);
		const day = startOfDay(date);
		const group = groups.at(-1);
		if (group?.day === day) {
			group.events.push(event);
		} else {
			groups.push({ day, date, events: [event] });
		}
	}
	return groups;
}

export function HistoryTimeline({ events }: { events: ApplicationEvent[] }) {
	return (
		<div className="ml-1.5 space-y-6 border-l border-border">
			{groupByDay(events).map((group) => (
				<section key={group.day} className="space-y-3">
					<h2 className="pl-6 text-xs font-medium tracking-wide text-muted-foreground uppercase">
						{formatDay(group.date)}
					</h2>
					<ol>
						{group.events.map((event) => (
							<HistoryEvent key={event.id} event={event} />
						))}
					</ol>
				</section>
			))}
		</div>
	);
}

function HistoryEvent({ event }: { event: ApplicationEvent }) {
	const createdAt = new Date(event.createdAt);
	const timeFormatter = new Intl.DateTimeFormat(getLocale(), { timeStyle: "short" });
	const duration = event.completedAt
		? formatDuration(new Date(event.completedAt).getTime() - createdAt.getTime())
		: null;

	return (
		<li className="relative pb-5 pl-6 last:pb-1">
			{/* The opaque backing keeps the line from showing through translucent or pulsing dots. */}
			<span
				className="absolute top-1.5 -left-[6.5px] size-3 rounded-full bg-background ring-4 ring-background"
				aria-hidden="true"
			>
				<span
					className={cn(
						"block size-full rounded-full",
						dotClasses[event.status] ?? "bg-muted-foreground/40",
					)}
				/>
			</span>
			<div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
				<div className="flex items-center gap-2">
					<span className="font-medium">{eventTypeLabel(event.type)}</span>
					<ApplicationEventStatusBadge status={event.status} />
				</div>
				<span className="text-xs whitespace-nowrap text-muted-foreground tabular-nums">
					<InfoTooltip>
						<InfoTooltipTrigger
							render={<time dateTime={createdAt.toISOString()} />}
							nativeButton={false}
							className="cursor-help"
						>
							{timeFormatter.format(createdAt)}
						</InfoTooltipTrigger>
						<InfoTooltipContent>{createdAt.toLocaleString(getLocale())}</InfoTooltipContent>
					</InfoTooltip>
					{duration ? ` · ${duration}` : null}
				</span>
			</div>
			<p className="mt-1 flex min-w-0 flex-wrap items-baseline gap-x-1.5 text-sm text-muted-foreground">
				<span>{eventSourceLabel(event.source)}</span>
				{event.actorName ? (
					<>
						<span aria-hidden="true">·</span>
						<span className="text-foreground">{event.actorName}</span>
					</>
				) : null}
				{event.commitHash ? (
					<>
						<span aria-hidden="true">·</span>
						<span className="font-mono text-xs text-foreground select-all">
							{event.commitHash.slice(0, 7)}
						</span>
						{event.commitMessage ? (
							<span className="max-w-full truncate" title={event.commitMessage}>
								{event.commitMessage}
							</span>
						) : null}
					</>
				) : null}
			</p>
			{event.errorMessage ? (
				<pre className="mt-2 max-h-48 overflow-auto rounded-md border border-destructive/30 bg-destructive/5 p-3 font-mono text-xs whitespace-pre-wrap text-destructive wrap-break-word">
					{event.errorMessage}
				</pre>
			) : null}
		</li>
	);
}
