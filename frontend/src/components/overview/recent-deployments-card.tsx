import { Link } from "@tanstack/react-router";
import { ApplicationEventStatusBadge } from "@/components/badges/application-event-status-badge";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
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
import { eventSourceLabel, eventTypeLabel } from "@/lib/application-events";
import type { ApplicationActivityDeployment } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { formatRelativeTime } from "@/lib/utils";
import { useApplicationActivity } from "./activity";

export function RecentDeploymentsCard() {
	const { data } = useApplicationActivity();

	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.overviewRecentDeploymentsTitle()}</CardTitle>
				<CardDescription>{m.overviewRecentDeploymentsDescription()}</CardDescription>
			</CardHeader>
			<CardContent>
				{!data ? (
					<div className="space-y-2">
						{Array.from({ length: 4 }, (_, index) => (
							<Skeleton key={index} className="h-12 w-full" />
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
		deployment.commitHash?.slice(0, 7),
	].filter(Boolean);
	const description = deployment.errorMessage ?? deployment.commitMessage;

	return (
		<Item
			size="xs"
			render={<Link to="/applications/$id/history" params={{ id: deployment.applicationId }} />}
		>
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
				<span className="text-xs whitespace-nowrap text-muted-foreground">
					{formatRelativeTime(deployment.createdAt)}
				</span>
				<ApplicationEventStatusBadge status={deployment.status} />
			</ItemActions>
		</Item>
	);
}
