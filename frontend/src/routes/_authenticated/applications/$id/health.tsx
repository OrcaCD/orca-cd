import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { z } from "zod";
import { EmptyState } from "@/components/empty-state";
import ErrorAlert from "@/components/alerts/error-alert";
import { ApplicationStatusBadge } from "@/components/badges/application-status-badge";
import { AvailabilityBar } from "@/components/applications/health/availability-bar";
import { HealthIncidents } from "@/components/applications/health/health-incidents";
import { HealthStatCards } from "@/components/applications/health/health-stat-cards";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useFetch } from "@/lib/api";
import {
	formatHealthDuration,
	healthRanges,
	useApplicationHealth,
	type HealthRange,
} from "@/lib/application-health";
import { HealthStatus, type Application } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { Activity, AlertTriangleIcon } from "lucide-react";

const healthSearchSchema = z.object({
	range: z.enum(healthRanges).optional(),
});

const DEFAULT_RANGE: HealthRange = "30d";

export const Route = createFileRoute("/_authenticated/applications/$id/health")({
	component: ApplicationHealthPage,
	validateSearch: (search) => healthSearchSchema.safeParse(search).data ?? {},
	head: () => ({
		meta: [
			{
				title: `${m.pageApplications()} - ${m.healthTitle()}`,
			},
		],
	}),
});

function ApplicationHealthPage() {
	const { id } = Route.useParams();
	const { range = DEFAULT_RANGE } = Route.useSearch();
	const navigate = useNavigate({ from: Route.fullPath });
	const { data: application, error: applicationError } = useFetch<Application>(
		`/applications/${id}`,
	);
	const {
		data: stats,
		error: statsError,
		isLoading,
	} = useApplicationHealth(id, range, application?.healthStatus);
	const error = statsError ?? applicationError;

	const current = stats?.current;
	const liveStatus = application?.healthStatus ?? current?.status ?? HealthStatus.Unknown;
	const noHistory =
		stats &&
		!stats.current &&
		!stats.truncated &&
		stats.healthySeconds === 0 &&
		stats.downtimeSeconds === 0 &&
		stats.noDataSeconds === 0 &&
		stats.incidents.length === 0 &&
		stats.buckets.every((bucket) => bucket.status === null);

	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-wrap items-center justify-between gap-4">
				<div className="flex flex-wrap items-center gap-3">
					<h1 className="text-2xl leading-8 font-semibold tracking-tight">{m.healthTitle()}</h1>
					{stats ? (
						<>
							<ApplicationStatusBadge type="health" status={liveStatus} />
							{current && current.status === liveStatus ? (
								<span className="text-sm text-muted-foreground">
									{m.healthSince({
										duration: formatHealthDuration(
											(new Date(stats.to).getTime() - new Date(current.since).getTime()) / 1000,
										),
									})}
								</span>
							) : null}
						</>
					) : null}
				</div>
				{!noHistory && (
					<Tabs
						value={range}
						onValueChange={(value) => {
							const next = healthRanges.find((candidate) => candidate === value);
							if (next) {
								void navigate({ search: { range: next }, replace: true });
							}
						}}
					>
						<TabsList>
							{healthRanges.map((candidate) => (
								<TabsTrigger key={candidate} value={candidate} className="px-3 tabular-nums">
									{candidate}
								</TabsTrigger>
							))}
						</TabsList>
					</Tabs>
				)}
			</div>

			{error ? (
				<ErrorAlert title={m.healthLoadFailed()} description={(error as Error).message} />
			) : isLoading || !stats ? (
				<div className="flex flex-col gap-4">
					<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
						<Skeleton className="h-28" />
						<Skeleton className="h-28" />
						<Skeleton className="h-28" />
						<Skeleton className="h-28" />
					</div>
					<Skeleton className="h-44 w-full" />
					<Skeleton className="h-56 w-full" />
				</div>
			) : noHistory ? (
				<EmptyState
					icon={Activity}
					title={m.healthHistoryEmptyTitle()}
					description={m.healthHistoryEmptyDescription()}
				/>
			) : (
				<>
					{stats.truncated ? (
						<Alert className="border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-50">
							<AlertTriangleIcon />
							<AlertTitle>{m.healthTruncatedTitle()}</AlertTitle>
							<AlertDescription className="text-foreground text-wrap">
								{m.healthTruncatedDescription()}
							</AlertDescription>
						</Alert>
					) : null}
					<HealthStatCards stats={stats} />
					<AvailabilityBar stats={stats} />
					<HealthIncidents stats={stats} agentName={application?.agentName ?? ""} />
				</>
			)}
		</div>
	);
}
