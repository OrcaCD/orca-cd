import { createFileRoute } from "@tanstack/react-router";
import useSWRInfinite from "swr/infinite";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import ErrorAlert from "@/components/alerts/error-alert";
import { columns } from "@/components/tables/application-events/columns";
import { ApplicationEventsDataTable } from "@/components/tables/application-events/data-table";
import { API_BASE, fetcher } from "@/lib/api";
import type { ApplicationEventsPage } from "@/lib/application-events";
import { m } from "@/lib/paraglide/messages";

export const Route = createFileRoute("/_authenticated/applications/$id/history")({
	component: ApplicationHistoryPage,
	head: () => ({
		meta: [
			{
				title: `${m.pageApplications()} - ${m.applicationHistory()}`,
			},
		],
	}),
});

const APPLICATION_EVENTS_LIMIT = 20;

const getKey =
	(id: string) => (pageIndex: number, previousPageData: ApplicationEventsPage | null) => {
		if (previousPageData && !previousPageData.hasMore) {
			return null;
		}
		const offset = pageIndex * APPLICATION_EVENTS_LIMIT;
		return `${API_BASE}/applications/${id}/events?limit=${APPLICATION_EVENTS_LIMIT}&offset=${offset}`;
	};

// Keys include API_BASE so the SSE broker's update events (which publish full
// API paths) revalidate every loaded page of this list.
function ApplicationHistoryPage() {
	const { id } = Route.useParams();
	const { data, error, isLoading, isValidating, size, setSize } =
		useSWRInfinite<ApplicationEventsPage>(getKey(id), fetcher<ApplicationEventsPage>);

	const events = data?.flatMap((page) => page.items) ?? [];
	const hasMore = data?.at(-1)?.hasMore ?? false;
	const loadingMore = isValidating && Boolean(data) && data?.[size - 1] === undefined;

	if (isLoading) {
		return (
			<div className="flex flex-col gap-2">
				<Skeleton className="h-10 w-full" />
				<Skeleton className="h-10 w-full" />
				<Skeleton className="h-10 w-full" />
			</div>
		);
	}

	if (error) {
		return (
			<ErrorAlert title={m.applicationHistoryLoadFailed()} description={(error as Error).message} />
		);
	}

	return (
		<div className="flex flex-col gap-6">
			<h1 className="text-2xl font-bold">{m.applicationHistory()}</h1>
			<ApplicationEventsDataTable columns={columns} data={events} />

			{hasMore ? (
				<Button variant="outline" disabled={loadingMore} onClick={() => setSize(size + 1)}>
					{loadingMore ? m.applicationHistoryLoadingMore() : m.loadMore()}
				</Button>
			) : (
				events.length > 0 && (
					<p className="text-sm text-muted-foreground text-center">{m.applicationHistoryEnd()}</p>
				)
			)}
		</div>
	);
}
