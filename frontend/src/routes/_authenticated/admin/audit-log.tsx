import { createFileRoute } from "@tanstack/react-router";
import { m } from "@/lib/paraglide/messages";
import { AuditLogsDataTable } from "@/components/tables/audit-log/data-table";
import { columns } from "@/components/tables/audit-log/columns";
import useSWRInfinite from "swr/infinite";
import { fetcher } from "@/lib/api";
import { Button } from "@base-ui/react";

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

const getKey = (pageIndex: number, previousPageData: any) => {
	if (previousPageData && !previousPageData.hasMore) {
		return null;
	}
	const offset = pageIndex * AUDIT_LOG_LIMIT;
	return `/admin/audit-logs?limit=${AUDIT_LOG_LIMIT}&offset=${offset}`;
};

function AuditLogPage() {
	const { data, size, setSize } = useSWRInfinite(getKey, fetcher);

	const logs = data ? data.flatMap((page) => page.items) : [];
	const lastPage = data?.[data.length - 1];
	const hasMore = lastPage?.hasMore;

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h1 className="text-2xl font-bold">{m.adminAuditLog()}</h1>
				<p className="text-muted-foreground text-sm">{m.adminAuditLogDescription()}</p>
			</div>

			<AuditLogsDataTable columns={columns} data={logs} />

			{hasMore ? (
				<Button onClick={() => setSize(size + 1)}>{m.loadMore()}</Button>
			) : (
				<p className="text-sm text-muted-foreground text-center">{m.noMoreLogs()}</p>
			)}
		</div>
	);
}
