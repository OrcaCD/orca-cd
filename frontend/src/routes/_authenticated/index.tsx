import { createFileRoute } from "@tanstack/react-router";
import { OverviewDashboard } from "@/components/overview/overview-dashboard";
import { OnboardingChecklist } from "@/components/overview/onboarding-checklist";
import { Skeleton } from "@/components/ui/skeleton";
import { PageContainer, PageHeader } from "@/components/page-header";
import type { Agent } from "@/lib/agents";
import { useFetch } from "@/lib/api";
import type { ApplicationListItem } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { usePermissions } from "@/lib/permissions";

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
	const { can } = usePermissions();

	return (
		<PageContainer>
			<PageHeader title={m.pageOverview()} description={m.overviewPageDescription()} />
			{!apps ? (
				<div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
					<Skeleton className="h-44 md:col-span-2 xl:col-span-1" />
					<Skeleton className="h-44" />
					<Skeleton className="h-44" />
				</div>
			) : apps.length === 0 && can("application", "create") ? (
				<OnboardingChecklist agents={agents} />
			) : (
				<OverviewDashboard apps={apps} agents={agents} />
			)}
		</PageContainer>
	);
}
