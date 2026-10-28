import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { z } from "zod";
import ErrorAlert from "@/components/alerts/error-alert";
import { ApplicationStatusBadge } from "@/components/badges/application-status-badge";
import { AvailabilityBar } from "@/components/applications/health/availability-bar";
import { HealthIncidents } from "@/components/applications/health/health-incidents";
import { HealthStatCards } from "@/components/applications/health/health-stat-cards";
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
	const { data: application } = useFetch<Application>(`/applications/${id}`);
	const { data: stats, error, isLoading } = useApplicationHealth(id, range);

	const current = stats?.current;

	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-wrap items-center justify-between gap-4">
				<div className="flex flex-wrap items-center gap-3">
					<h1 className="text-2xl font-bold">{m.healthTitle()}</h1>
					{stats ? (
						<>
							<ApplicationStatusBadge
								type="health"
								status={current?.status ?? HealthStatus.Unknown}
							/>
							{current ? (
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
			) : (
				<>
					<HealthStatCards stats={stats} />
					<AvailabilityBar stats={stats} />
					<HealthIncidents stats={stats} agentName={application?.agentName ?? ""} />
				</>
			)}
		</div>
	);
}
