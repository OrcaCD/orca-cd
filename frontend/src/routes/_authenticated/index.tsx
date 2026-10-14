import { createFileRoute } from "@tanstack/react-router";
import { ApplicationsOverview } from "@/components/applications-overview/applications-overview";
import UpsertApplicationDialog from "@/components/dialogs/upsert-application";
import { Skeleton } from "@/components/ui/skeleton";
import type { Agent } from "@/lib/agents";
import { useFetch } from "@/lib/api";
import type { ApplicationListItem } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";

export const Route = createFileRoute("/_authenticated/")({
	component: OverviewPage,
	head: () => ({
		meta: [
			{
				title: m.pageOverview(),
			},
		],
	}),
});

function OverviewPage() {
	const { data: apps } = useFetch<ApplicationListItem[]>("/applications");
	const { data: agents } = useFetch<Agent[]>("/agents");

	return (
		<div className="p-6 space-y-6">
			<div>
				<h1 className="text-2xl font-bold">{m.pageOverview()}</h1>
				<p className="text-muted-foreground text-sm mt-1">{m.overviewPageDescription()}</p>
			</div>
			{!apps ? (
				<div className="grid gap-4 md:grid-cols-2 xl:grid-cols-[3fr_2fr_2fr]">
					<Skeleton className="h-44 md:col-span-2 xl:col-span-1" />
					<Skeleton className="h-44" />
					<Skeleton className="h-44" />
				</div>
			) : apps.length === 0 ? (
				<div className="flex flex-col items-center gap-4 rounded-xl border border-dashed p-10 text-center">
					<div>
						<p className="text-sm font-medium">{m.noApplicationsFound()}</p>
						<p className="mt-1 text-sm text-muted-foreground">
							{m.overviewNoApplicationsDescription()}
						</p>
					</div>
					<UpsertApplicationDialog application={null} />
				</div>
			) : (
				<ApplicationsOverview apps={apps} agents={agents} />
			)}
		</div>
	);
}
