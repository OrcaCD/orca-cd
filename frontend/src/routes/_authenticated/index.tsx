import { createFileRoute } from "@tanstack/react-router";
import { ApplicationsOverview } from "@/components/applications-overview/applications-overview";
import { OnboardingChecklist } from "@/components/applications-overview/onboarding-checklist";
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
				<OnboardingChecklist agents={agents} />
			) : (
				<ApplicationsOverview apps={apps} agents={agents} />
			)}
		</div>
	);
}
