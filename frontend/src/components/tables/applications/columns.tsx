import type { ColumnDef } from "@tanstack/react-table";
import { MoreHorizontal, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { DataTableColumnHeader } from "../data-table-column-header";
import type { ApplicationListItem } from "@/lib/applications";
import { Link } from "@tanstack/react-router";
import { StatusBadge } from "@/components/status-badge";
import { m } from "@/lib/paraglide/messages";

export const columns: ColumnDef<ApplicationListItem>[] = [
	{
		accessorKey: "name",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnName()} />;
		},
		cell: ({ row }) => {
			const app = row.original;
			return (
				<Link
					to="/applications/$id"
					params={{ id: app.id }}
					className="font-medium hover:text-primary underline underline-offset-2"
				>
					{app.name}
				</Link>
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
			return <StatusBadge status={app.syncStatus} type="sync" />;
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
			return <StatusBadge status={app.healthStatus} type="health" />;
		},
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
		cell: () => {
			return (
				<div className="flex justify-end">
					<DropdownMenu>
						<DropdownMenuTrigger asChild onClick={(e) => e.preventDefault()}>
							<Button variant="ghost" size="icon" className="h-8 w-8">
								<MoreHorizontal className="h-4 w-4" />
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end" className="w-full">
							<DropdownMenuItem>
								<RefreshCw className="mr-2 h-4 w-4" />
								{m.sync()}
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				</div>
			);
		},
	},
];
