import { SearchField } from "@/components/search-field";
import { createFileRoute } from "@tanstack/react-router";
import { m } from "@/lib/paraglide/messages";
import { AuditLogsDataTable } from "@/components/tables/audit-log/data-table";
import { columns } from "@/components/tables/audit-log/columns";
import useSWRInfinite from "swr/infinite";
import { fetcher } from "@/lib/api";
import { Button } from "@base-ui/react";
import { useMemo, useState } from "react";
import { useDebouncedValue } from "@/hooks/use-debounced-value";

export const Route = createFileRoute("/_authenticated/admin/audit-log")({
	component: AuditLogPage,
	head: () => ({
		meta: [
			{
				title: `${m.admin()} - ${m.adminAuditLog()}`,
			},
		],
	}),
});

const AUDIT_LOG_LIMIT = 20;

const getKey = (search: string) => (pageIndex: number, previousPageData: any) => {
	if (previousPageData && !previousPageData.hasMore) {
		return null;
	}
	const offset = pageIndex * AUDIT_LOG_LIMIT;
	const searchParam = search ? `&search=${encodeURIComponent(search)}` : "";
	return `/admin/audit-logs?limit=${AUDIT_LOG_LIMIT}&offset=${offset}${searchParam}`;
};

function AuditLogPage() {
	const [search, setSearch] = useState("");
	const debouncedSearch = useDebouncedValue(search.trim());
	const { data, size, setSize } = useSWRInfinite(getKey(debouncedSearch), fetcher, {
		keepPreviousData: true,
	});

	const logs = useMemo(() => (data ? data.flatMap((page) => page.items) : []), [data]);
	const lastPage = data?.[data.length - 1];
	const hasMore = lastPage?.hasMore;

	return (
		<div className="flex flex-col gap-6">
			<SearchField
				value={search}
				onChange={setSearch}
				label={m.searchAuditLogLabel()}
				placeholder={m.searchAuditLog()}
				className="w-md max-w-full"
			/>

			<AuditLogsDataTable columns={columns} data={logs} />

			{hasMore ? (
				<Button onClick={() => setSize(size + 1)}>{m.loadMore()}</Button>
			) : (
				<p className="text-sm text-muted-foreground text-center">{m.noMoreLogs()}</p>
			)}
		</div>
	);
}
