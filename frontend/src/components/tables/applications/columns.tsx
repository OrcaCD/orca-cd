import type { ColumnDef } from "@tanstack/react-table";
import { MoreHorizontal } from "lucide-react";
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

export const columns: ColumnDef<ApplicationListItem>[] = [
	{
		accessorKey: "name",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Name" />;
		},
		cell: ({ row }) => {
			const app = row.original;
			return (
				<Link
					to="/applications/$id"
					params={{ id: app.id }}
					className="font-medium hover:text-primary"
				>
					{app.name}
				</Link>
			);
		},
	},
	{
		id: "syncStatus",
		accessorFn: (row) => row.syncStatus,
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Sync Status" />;
		},
		cell: ({ row }) => {
			const app = row.original;
			return <StatusBadge status={app.syncStatus} type="sync" />;
		},
	},
	{
		id: "healthStatus",
		accessorFn: (row) => row.healthStatus,
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Health Status" />;
		},
		cell: ({ row }) => {
			const app = row.original;
			return <StatusBadge status={app.healthStatus} type="health" />;
		},
	},
	{
		accessorKey: "repositoryName",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Repository" />;
		},
	},
	{
		accessorKey: "agentName",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Agent" />;
		},
	},
	{
		accessorKey: "branch",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Branch" />;
		},
	},
	{
		accessorKey: "path",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Path" />;
		},
	},
	{
		accessorKey: "lastSyncedAt",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Last Sync" />;
		},
		cell: ({ row }) => {
			const app = row.original;
			return app.lastSyncedAt ? new Date(app.lastSyncedAt).toLocaleString() : "Never";
		},
	},
	{
		id: "actions",
		cell: ({ row }) => {
			return (
				<div className="flex justify-end">
					<DropdownMenu>
						<DropdownMenuTrigger asChild onClick={(e) => e.preventDefault()}>
							<Button variant="ghost" size="icon" className="h-8 w-8">
								<MoreHorizontal className="h-4 w-4" />
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end">
							<DropdownMenuItem>Sync</DropdownMenuItem>
							<DropdownMenuItem asChild>
								<Link to="/applications/$id/settings" params={{ id: row.original.id }}>
									Settings
								</Link>
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				</div>
			);
		},
	},
];
