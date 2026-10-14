import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { Search } from "lucide-react";
import useSWRInfinite from "swr/infinite";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import ErrorAlert from "@/components/alerts/error-alert";
import { columns } from "@/components/tables/application-events/columns";
import { ApplicationEventsDataTable } from "@/components/tables/application-events/data-table";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
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
	(id: string, search: string) =>
	(pageIndex: number, previousPageData: ApplicationEventsPage | null) => {
		if (previousPageData && !previousPageData.hasMore) {
			return null;
		}
		const offset = pageIndex * APPLICATION_EVENTS_LIMIT;
		const searchParam = search ? `&search=${encodeURIComponent(search)}` : "";
		return `${API_BASE}/applications/${id}/events?limit=${APPLICATION_EVENTS_LIMIT}&offset=${offset}${searchParam}`;
	};

// Keys include API_BASE so the SSE broker's update events (which publish full
// API paths) revalidate every loaded page of this list.
function ApplicationHistoryPage() {
	const { id } = Route.useParams();
	const [search, setSearch] = useState("");
	const debouncedSearch = useDebouncedValue(search.trim());
	const { data, error, isLoading, isValidating, size, setSize } =
		useSWRInfinite<ApplicationEventsPage>(
			getKey(id, debouncedSearch),
			fetcher<ApplicationEventsPage>,
			{ keepPreviousData: true },
		);

	const events = data?.flatMap((page) => page.items) ?? [];
	const hasMore = data?.at(-1)?.hasMore ?? false;
	const loadingMore = isValidating && Boolean(data) && data?.[size - 1] === undefined;

	return (
		<div className="flex flex-col gap-6">
			<h1 className="text-2xl font-bold">{m.applicationHistory()}</h1>
			<div className="relative w-md max-w-full">
				<Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
				<Input
					placeholder={m.searchApplicationHistory()}
					className="pl-9 bg-muted border-border"
					value={search}
					onChange={(event) => setSearch(event.target.value)}
				/>
			</div>

			{isLoading && !data ? (
				<div className="flex flex-col gap-2">
					<Skeleton className="h-10 w-full" />
					<Skeleton className="h-10 w-full" />
					<Skeleton className="h-10 w-full" />
				</div>
			) : error ? (
				<ErrorAlert
					title={m.applicationHistoryLoadFailed()}
					description={(error as Error).message}
				/>
			) : (
				<>
					<ApplicationEventsDataTable
						columns={columns}
						data={events}
						emptyMessage={debouncedSearch ? m.noResults() : undefined}
					/>

					{hasMore ? (
						<Button variant="outline" disabled={loadingMore} onClick={() => setSize(size + 1)}>
							{loadingMore ? m.applicationHistoryLoadingMore() : m.loadMore()}
						</Button>
					) : (
						events.length > 0 && (
							<p className="text-sm text-muted-foreground text-center">
								{m.applicationHistoryEnd()}
							</p>
						)
					)}
				</>
			)}
		</div>
	);
}
