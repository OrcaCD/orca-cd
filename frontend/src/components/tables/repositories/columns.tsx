import {
	deleteRepository,
	getGitProviderIconPath,
	type Repository,
	type RepositorySyncStatus,
	type RepositorySyncType,
} from "@/lib/repsitories";
import type { ColumnDef } from "@tanstack/react-table";
import {
	ExternalLink,
	MoreHorizontal,
	MousePointerClickIcon,
	RefreshCw,
	Trash2,
	WebhookIcon,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { DataTableColumnHeader } from "../data-table-column-header";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import UpsertRepositoryDialog from "@/components/dialogs/upsert-repository";
import { toast } from "sonner";

function getLastSyncSearchText(lastSync?: string | null): string {
	if (!lastSync) {
		return "N/A not synced";
	}

	const parsedDate = new Date(lastSync);
	if (Number.isNaN(parsedDate.getTime())) {
		return "N/A not synced";
	}

	return [
		parsedDate.toISOString(),
		parsedDate.toLocaleDateString(),
		parsedDate.toLocaleTimeString(),
		parsedDate.toLocaleString(),
	].join(" ");
}

function getSyncStatusColor(syncStatus: RepositorySyncStatus): string {
	switch (syncStatus) {
		case "syncing":
			return "bg-blue-500";
		case "failed":
			return "bg-red-500";
		case "success":
			return "bg-green-500";
		default:
			return "bg-gray-500";
	}
}

function getSyncTypeIcon(syncType: RepositorySyncType) {
	switch (syncType) {
		case "webhook":
			return <WebhookIcon className="h-4 w-4" />;
		case "polling":
			return <RefreshCw className="h-4 w-4" />;
		case "manual":
			return <MousePointerClickIcon className="h-4 w-4" />;
		default:
			return null;
	}
}

export const columns: ColumnDef<Repository>[] = [
	{
		accessorKey: "name",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Name" />;
		},
		cell: ({ row }) => {
			const name = row.original.name;
			const url = row.original.url;
			const provider = row.original.provider;

			return (
				<div className="flex flex-row gap-3 items-center">
					<img src={getGitProviderIconPath(provider)} alt="Git Provider" className="h-7 w-7" />

					<div>
						<p className="font-medium">{name}</p>
						<a
							href={url}
							target="_blank"
							rel="noopener noreferrer"
							className="text-sm text-muted-foreground hover:text-primary flex items-center gap-1"
						>
							{url}
							<ExternalLink className="h-3 w-3" />
						</a>
					</div>
				</div>
			);
		},
	},
	{
		id: "syncStatus",
		accessorFn: (row) => row.syncStatus,
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Status" />;
		},
		cell: ({ row }) => {
			const syncStatus = row.original.syncStatus;

			return (
				<div className="flex items-center gap-2">
					<span className={`inline-flex h-2 w-2 rounded-full ${getSyncStatusColor(syncStatus)}`} />
					{syncStatus.charAt(0).toUpperCase() + syncStatus.slice(1)}
				</div>
			);
		},
	},
	{
		id: "syncType",
		accessorFn: (row) => row.syncType,
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Sync Type" />;
		},
		cell: ({ row }) => {
			const syncType = row.original.syncType;
			return (
				<div className="flex items-center gap-2">
					{getSyncTypeIcon(syncType)}
					{syncType.charAt(0).toUpperCase() + syncType.slice(1)}
				</div>
			);
		},
	},
	{
		id: "lastSyncedAt",
		accessorFn: (row) => getLastSyncSearchText(row.lastSyncedAt),
		sortingFn: (rowA, rowB) => {
			const firstSync = rowA.original.lastSyncedAt
				? new Date(rowA.original.lastSyncedAt).getTime()
				: 0;
			const secondSync = rowB.original.lastSyncedAt
				? new Date(rowB.original.lastSyncedAt).getTime()
				: 0;

			return firstSync - secondSync;
		},
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Last Sync" />;
		},
		cell: ({ row }) => {
			const lastSyncedAt = row.original.lastSyncedAt;

			return <span>{lastSyncedAt ? new Date(lastSyncedAt).toLocaleString() : "Never"}</span>;
		},
	},
	{
		accessorKey: "authMethod",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Auth Method" />;
		},
	},
	{
		accessorKey: "apps",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Apps" />;
		},
	},
	{
		id: "actions",
		cell: ({ row }) => {
			async function handleDelete() {
				try {
					await deleteRepository(row.original.id);
					toast.success(`Repository ${row.original.name} deleted successfully`);
				} catch (err) {
					toast.error(err instanceof Error ? err.message : "Failed to delete repository");
				}
			}

			return (
				<div className="flex justify-end">
					<DropdownMenu>
						<DropdownMenuTrigger asChild>
							<Button variant="ghost" className="h-8 w-8 p-0">
								<span className="sr-only">Open menu</span>
								<MoreHorizontal className="h-4 w-4" />
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end">
							<DropdownMenuLabel>Actions</DropdownMenuLabel>
							<DropdownMenuItem>
								<RefreshCw className="h-4 w-4" />
								Refresh
							</DropdownMenuItem>
							<DropdownMenuSeparator />
							<UpsertRepositoryDialog repository={row.original} asDropdownItem />
							<ConfirmationDialog
								triggerText={
									<>
										<Trash2 className="h-4 w-4" />
										Disconnect
									</>
								}
								onConfirm={async () => await handleDelete()}
								asDropdownItem
							/>
						</DropdownMenuContent>
					</DropdownMenu>
				</div>
			);
		},
	},
];
