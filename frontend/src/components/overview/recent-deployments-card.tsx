import { Link } from "@tanstack/react-router";
import { ApplicationEventStatusBadge } from "@/components/badges/application-event-status-badge";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import { History } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import {
	Item,
	ItemActions,
	ItemContent,
	ItemDescription,
	ItemGroup,
	ItemMedia,
} from "@/components/ui/item";
import { Skeleton } from "@/components/ui/skeleton";
import { ApplicationEventStatus, eventSourceLabel, eventTypeLabel } from "@/lib/application-events";
import { cn } from "@/lib/utils";
import type { ApplicationActivityDeployment } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { RelativeTime } from "@/components/relative-time";
import { useApplicationActivity } from "./activity";
import { OverviewCardHeader } from "./overview-card";

export function RecentDeploymentsCard() {
	const { data } = useApplicationActivity();

	return (
		<Card>
			<OverviewCardHeader
				icon={History}
				title={m.overviewRecentDeploymentsTitle()}
				description={m.overviewRecentDeploymentsDescription()}
			/>
			<CardContent>
				{!data ? (
					<div className="space-y-2">
						{Array.from({ length: 4 }, (_, index) => (
							<div key={index} className="flex items-start gap-2.5 px-2.5 py-2" aria-hidden="true">
								<Skeleton className="mt-1 size-8 shrink-0 rounded-md" />
								<div className="flex flex-1 flex-col gap-2">
									<div className="flex justify-between gap-4">
										<Skeleton className="h-4 w-1/3" />
										<Skeleton className="h-5 w-16 rounded-full" />
									</div>
									<Skeleton className="h-3 w-3/4" />
									<Skeleton className="h-3 w-2/3" />
								</div>
							</div>
						))}
					</div>
				) : data.recentDeployments.length === 0 ? (
					<p className="text-sm text-muted-foreground">{m.applicationsActivityNoDeployments()}</p>
				) : (
					<ItemGroup className="gap-0">
						{data.recentDeployments.map((deployment) => (
							<RecentDeploymentRow key={deployment.id} deployment={deployment} />
						))}
					</ItemGroup>
				)}
			</CardContent>
		</Card>
	);
}

function RecentDeploymentRow({ deployment }: { deployment: ApplicationActivityDeployment }) {
	const details = [
		eventTypeLabel(deployment.type),
		deployment.actorName ?? eventSourceLabel(deployment.source),
	].filter(Boolean);
	const description = deployment.errorMessage ?? deployment.commitMessage;
	const failed = deployment.status === ApplicationEventStatus.Failed;
	return (
		<Item size="xs" className="relative items-start hover:bg-muted/40">
			<Link
				to="/applications/$id/history"
				params={{ id: deployment.applicationId }}
				aria-label={deployment.applicationName}
				className="absolute inset-0 rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-ring"
			/>
			<ItemMedia className="mt-0.5 size-8 rounded-md bg-muted ring-1 ring-foreground/5 ring-inset">
				<StaticLucideIcon name={deployment.applicationIcon} className="size-4 text-primary" />
			</ItemMedia>
			<ItemContent className="min-w-0">
				<div className="flex min-w-0 items-start justify-between gap-2">
					<span className="min-w-0 truncate text-sm leading-5 font-medium">
						{deployment.applicationName}
					</span>
					<ItemActions className="shrink-0 gap-3">
						<ApplicationEventStatusBadge status={deployment.status} />
					</ItemActions>
				</div>
				<div className="mt-0.5 flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
					{deployment.commitHash ? (
						<span className="shrink-0 font-mono">{deployment.commitHash.slice(0, 7)}</span>
					) : null}
					<span>{details.join(" · ")}</span>
					<RelativeTime value={deployment.createdAt} className="relative" />
				</div>
				{description ? (
					<ItemDescription
						className={cn("mt-1", failed ? "line-clamp-2 wrap-break-word" : "truncate")}
						title={description}
					>
						{description}
					</ItemDescription>
				) : null}
			</ItemContent>
		</Item>
	);
}
