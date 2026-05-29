import { createFileRoute } from "@tanstack/react-router";
import { m } from "@/lib/paraglide/messages";
import { AuditLogsDataTable } from "@/components/tables/audit-log/data-table";
import { columns } from "@/components/tables/audit-log/columns";
import useSWRInfinite from "swr/infinite";

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

const fetcher = async (url: string) => {
	const r = await fetch(url);
	return r.json();
};

const getKey = (pageIndex: number, previousPageData: any) => {
	if (pageIndex === 0) {
		return `/api/v1/admin/audit-logs?limit=20`;
	}

	if (!previousPageData?.hasMore) {
		return null;
	}

	const lastItem = previousPageData.items[previousPageData.items.length - 1];

	return `/api/v1/admin/audit-logs?limit=20&cursor=${encodeURIComponent(lastItem.createdAt)}`;
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
				<button onClick={() => setSize(size + 1)}>{m.loadMore()}</button>
			) : (
				<p className="text-sm text-muted-foreground text-center">{m.noMoreLogs()}</p>
			)}
		</div>
	);
}
