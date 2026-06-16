import type { ColumnDef } from "@tanstack/react-table";
import { ArrowRight } from "lucide-react";
import { DataTableColumnHeader } from "../data-table-column-header";
import type { ApplicationListItem } from "@/lib/applications";
import { Link } from "@tanstack/react-router";
import { ApplicationStatusBadge } from "@/components/badges/application-status-badge";
import { m } from "@/lib/paraglide/messages";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";

export const columns: ColumnDef<ApplicationListItem>[] = [
	{
		accessorKey: "name",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnName()} />;
		},
		cell: ({ row }) => {
			const app = row.original;

			return (
				<div className="flex flex-row gap-3 items-center">
					<div className="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
						<StaticLucideIcon name={app.icon} className="h-5 w-5 text-primary" />
					</div>

					<div>{app.name}</div>
				</div>
			);
		},
	},
	{
		id: "sync status",
		accessorFn: (row) => row.syncStatus,
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnSyncStatus()} />;
		},
		cell: ({ row }) => {
			const app = row.original;
			return <ApplicationStatusBadge status={app.syncStatus} type="sync" />;
		},
	},
	{
		id: "health status",
		accessorFn: (row) => row.healthStatus,
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnHealthStatus()} />;
		},
		cell: ({ row }) => {
			const app = row.original;
			return <ApplicationStatusBadge status={app.healthStatus} type="health" />;
		},
	},
	{
		id: "commit",
		accessorKey: "commit",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnCommit()} />;
		},
		cell: ({ row }) => row.original.commit.slice(0, 7),
	},
	{
		id: "repository",
		accessorKey: "repositoryName",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnRepository()} />;
		},
	},
	{
		id: "agent",
		accessorKey: "agentName",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnAgent()} />;
		},
	},
	{
		accessorKey: "branch",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnBranch()} />;
		},
	},
	{
		accessorKey: "path",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnPath()} />;
		},
	},
	{
		id: "last sync",
		accessorKey: "lastSyncedAt",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnLastSync()} />;
		},
		cell: ({ row }) => {
			const app = row.original;
			return app.lastSyncedAt ? new Date(app.lastSyncedAt).toLocaleString() : m.never();
		},
	},
	{
		id: "actions",
		cell: ({ row }) => {
			const app = row.original;
			return (
				<Link
					to="/applications/$id"
					params={{ id: app.id }}
					className="flex items-center font-medium hover:text-primary underline underline-offset-2"
				>
					<ArrowRight className="mr-2 h-4 w-4" />
					{m.details()}
				</Link>
			);
		},
	},
];
