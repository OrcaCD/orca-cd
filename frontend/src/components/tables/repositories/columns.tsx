import {
	deleteRepository,
	getGitProviderIconPath,
	getGitProviderIconClass,
	type Repository,
	syncRepository,
} from "@/lib/repositories";
import type { ColumnDef } from "@tanstack/react-table";
import { ExternalLink, MoreHorizontal, RefreshCw, Trash2 } from "lucide-react";
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
import { toSearchableText } from "@/lib/utils";
import { m } from "@/lib/paraglide/messages";
import { RepositoryStatusBadge } from "@/components/badges/repository-status-badge";
import { RepositorySyncTypeBadge } from "@/components/badges/repository-sync-type-badge";

function getLastSyncSearchText(lastSync?: string | null): string {
	if (!lastSync) {
		return "N/A not synced never synced";
	}

	const parsedDate = new Date(lastSync);
	if (Number.isNaN(parsedDate.getTime())) {
		return "N/A not synced never synced";
	}

	return [
		parsedDate.toISOString(),
		parsedDate.toLocaleDateString(),
		parsedDate.toLocaleTimeString(),
		parsedDate.toLocaleString(),
	].join(" ");
}

export const columns: ColumnDef<Repository>[] = [
	{
		id: "name",
		accessorFn: (row) => `${row.provider} ${row.url} ${row.name}`,
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnName()} />;
		},
		cell: ({ row }) => {
			const name = row.original.name;
			const url = row.original.url;
			const provider = row.original.provider;

			return (
				<div className="flex flex-row gap-3 items-center">
					<img
						src={getGitProviderIconPath(provider)}
						alt={m.gitProviderAlt()}
						className={`h-7 w-7 ${getGitProviderIconClass(provider)}`}
					/>

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
			return <DataTableColumnHeader column={column} title={m.columnStatus()} />;
		},
		cell: ({ row }) => {
			const syncStatus = row.original.syncStatus;
			return <RepositoryStatusBadge status={syncStatus} />;
		},
	},
	{
		id: "syncType",
		accessorFn: (row) => row.syncType,
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnSyncType()} />;
		},
		cell: ({ row }) => {
			const syncType = row.original.syncType;
			return <RepositorySyncTypeBadge syncType={syncType} />;
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
			return <DataTableColumnHeader column={column} title={m.columnLastSync()} />;
		},
		cell: ({ row }) => {
			const lastSyncedAt = row.original.lastSyncedAt;

			return <span>{lastSyncedAt ? new Date(lastSyncedAt).toLocaleString() : m.never()}</span>;
		},
	},
	{
		accessorKey: "authMethod",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnAuthMethod()} />;
		},
	},
	{
		accessorKey: "appCount",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnApps()} />;
		},
	},
	{
		id: "createdAt",
		accessorFn: (row) => toSearchableText(row.createdAt),
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnCreatedAt()} />;
		},
		cell: ({ row }) => {
			const createdAt = row.original.createdAt;
			return <span>{new Date(createdAt).toLocaleString()}</span>;
		},
	},
	{
		id: "actions",
		cell: ({ row }) => {
			async function handleDelete() {
				try {
					await deleteRepository(row.original.id);
					toast.success(m.repositoryDeleted({ name: row.original.name }));
				} catch (err) {
					toast.error(err instanceof Error ? err.message : m.failedDeleteRepository());
				}
			}

			async function handleSyncRepo(repository: Repository) {
				try {
					await syncRepository(repository.id);
					toast.success(m.syncTriggered());
				} catch (err) {
					toast.error(err instanceof Error ? err.message : m.failedTriggerSync());
				}
			}

			return (
				<div className="flex justify-end">
					<DropdownMenu>
						<DropdownMenuTrigger asChild>
							<Button variant="ghost" className="h-8 w-8 p-0">
								<span className="sr-only">{m.openMenu()}</span>
								<MoreHorizontal className="h-4 w-4" />
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end">
							<DropdownMenuLabel>{m.actions()}</DropdownMenuLabel>
							<DropdownMenuItem onClick={() => handleSyncRepo(row.original)}>
								<RefreshCw className="h-4 w-4" />
								{m.sync()}
							</DropdownMenuItem>
							<DropdownMenuSeparator />
							<UpsertRepositoryDialog existingRepository={row.original} asDropdownItem />
							<ConfirmationDialog
								triggerText={
									<>
										<Trash2 className="h-4 w-4" />
										{m.delete()}
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
