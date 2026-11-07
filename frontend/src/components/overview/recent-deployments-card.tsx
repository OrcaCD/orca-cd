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
	ItemTitle,
} from "@/components/ui/item";
import { Skeleton } from "@/components/ui/skeleton";
import { ApplicationEventType, eventSourceLabel, eventTitle } from "@/lib/application-events";
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
		eventTitle(deployment),
		deployment.actorName ?? eventSourceLabel(deployment.source),
		// Rollback titles name their commit already.
		deployment.type === ApplicationEventType.Rollback ? null : deployment.commitHash?.slice(0, 7),
	].filter(Boolean);
	const description = deployment.errorMessage ?? deployment.commitMessage;

	return (
		<Item size="xs" className="relative hover:bg-muted/40">
			<Link
				to="/applications/$id/history"
				params={{ id: deployment.applicationId }}
				aria-label={deployment.applicationName}
				className="absolute inset-0 rounded-lg"
			/>
			<ItemMedia className="size-8 rounded-md bg-primary/10">
				<StaticLucideIcon name={deployment.applicationIcon} className="size-4 text-primary" />
			</ItemMedia>
			<ItemContent className="min-w-0">
				<ItemTitle className="w-full">
					<span className="truncate">{deployment.applicationName}</span>
					<span className="truncate text-xs font-normal text-muted-foreground">
						{details.join(" · ")}
					</span>
				</ItemTitle>
				{description ? (
					<ItemDescription className="truncate" title={description}>
						{description}
					</ItemDescription>
				) : null}
			</ItemContent>
			<ItemActions className="gap-3">
				<RelativeTime
					value={deployment.createdAt}
					className="relative text-xs text-muted-foreground"
				/>
				<ApplicationEventStatusBadge
					status={deployment.status}
					type={deployment.type}
					deploymentStatus={deployment.deploymentStatus}
					waitingForAgent={deployment.waitingForAgent}
				/>
			</ItemActions>
		</Item>
	);
}
