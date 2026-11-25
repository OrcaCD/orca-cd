import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Container, CircleArrowUp, Unplug } from "lucide-react";
import { z } from "zod";
import ErrorAlert from "@/components/alerts/error-alert";
import { ApplicationStatusBadge } from "@/components/badges/application-status-badge";
import { ContainerDetails } from "@/components/applications/monitoring/container-details";
import { ContainerPanel } from "@/components/applications/monitoring/container-panel";
import { ContainerStatCards } from "@/components/applications/monitoring/container-stat-cards";
import { ContainerStatePill } from "@/components/applications/monitoring/container-state";
import { ContainerSwitcher } from "@/components/applications/monitoring/container-switcher";
import { EmptyState } from "@/components/empty-state";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api";
import { findContainer, useApplicationContainers } from "@/lib/application-containers";
import { m } from "@/lib/paraglide/messages";

const monitoringSearchSchema = z.object({
	container: z.string().optional(),
});

export const Route = createFileRoute("/_authenticated/applications/$id/monitoring")({
	component: ApplicationMonitoringPage,
	validateSearch: (search) => monitoringSearchSchema.safeParse(search).data ?? {},
	head: () => ({
		meta: [
			{
				title: `${m.pageApplications()} - ${m.monitoringTitle()}`,
			},
		],
	}),
});

function ApplicationMonitoringPage() {
	const { id } = Route.useParams();
	const { container: containerParam } = Route.useSearch();
	const navigate = useNavigate({ from: Route.fullPath });
	const { data, error, isLoading } = useApplicationContainers(id);

	const containers = data?.containers ?? [];
	const selected = findContainer(containers, containerParam) ?? containers[0];

	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-col gap-4">
				<div className="flex flex-wrap items-center gap-3">
					<h1 className="text-2xl leading-8 font-semibold tracking-tight">{m.monitoringTitle()}</h1>
					{selected ? (
						<>
							<ContainerStatePill container={selected} />
							<ApplicationStatusBadge type="health" status={selected.health} />
							{selected.oomKilled ? (
								<Badge variant="destructive">{m.monitoringOomKilled()}</Badge>
							) : null}
						</>
					) : null}
				</div>
				{selected ? (
					<ContainerSwitcher
						containers={containers}
						selected={selected}
						onSelect={(key) => {
							void navigate({ search: { container: key }, replace: true });
						}}
					/>
				) : null}
			</div>

			{error && !data ? (
				<ContainersError error={error} />
			) : isLoading || !data ? (
				<div className="flex flex-col gap-4">
					<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
						<Skeleton className="h-28" />
						<Skeleton className="h-28" />
						<Skeleton className="h-28" />
						<Skeleton className="h-28" />
					</div>
					<Skeleton className="h-56 w-full" />
				</div>
			) : !selected ? (
				<EmptyState
					icon={Container}
					title={m.monitoringEmptyTitle()}
					description={m.monitoringEmptyDescription()}
				/>
			) : (
				<>
					{selected.error ? (
						<ErrorAlert title={m.monitoringStartError()} description={selected.error} />
					) : null}
					<ContainerStatCards applicationId={id} container={selected} />
					<ContainerDetails container={selected} />
					<ContainerPanel applicationId={id} container={selected} />
				</>
			)}
		</div>
	);
}

function ContainersError({ error }: { error: unknown }) {
	if (error instanceof ApiError && error.status === 503) {
		return (
			<EmptyState
				icon={Unplug}
				tone="destructive"
				title={m.monitoringAgentOfflineTitle()}
				description={m.monitoringAgentOfflineDescription()}
			/>
		);
	}

	if (error instanceof ApiError && error.status === 422) {
		return (
			<EmptyState
				icon={CircleArrowUp}
				title={m.monitoringAgentUnsupportedTitle()}
				description={m.monitoringAgentUnsupportedDescription()}
			/>
		);
	}

	return <ErrorAlert title={m.monitoringLoadFailed()} description={(error as Error).message} />;
}
