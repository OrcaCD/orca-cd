import type { ColumnDef } from "@tanstack/react-table";
import { MoreHorizontal, Send, Trash2 } from "lucide-react";
import { toast } from "sonner";

import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { NotificationStatusBadge } from "@/components/badges/notification-status-badge";
import { Button } from "@/components/ui/button";
import { DataTableColumnHeader } from "@/components/tables/data-table-column-header";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { m } from "@/lib/paraglide/messages";
import { deleteNotification, type Notification, testNotification } from "@/lib/notifications";

function formatDate(value: string): string {
	const parsed = new Date(value);
	if (Number.isNaN(parsed.getTime())) {
		return value;
	}

	return parsed.toLocaleString();
}

export const columns: ColumnDef<Notification>[] = [
	{
		accessorKey: "name",
		header: ({ column }) => <DataTableColumnHeader column={column} title={m.columnName()} />,
	},
	{
		id: "status",
		accessorFn: (row) => row.status,
		header: ({ column }) => <DataTableColumnHeader column={column} title={m.columnStatus()} />,
		cell: ({ row }) => <NotificationStatusBadge status={row.original.status} />,
	},
	{
		id: "type",
		accessorFn: (row) => row.type,
		header: ({ column }) => <DataTableColumnHeader column={column} title={m.columnType()} />,
		cell: ({ row }) => row.original.type.charAt(0).toUpperCase() + row.original.type.slice(1),
	},
	{
		id: "enabled",
		accessorFn: (row) => String(row.enabled),
		header: ({ column }) => <DataTableColumnHeader column={column} title={m.enabled()} />,
		cell: ({ row }) => (row.original.enabled ? m.enabled() : m.disabled()),
	},
	{
		id: "apps",
		accessorFn: (row) => row.applicationIds.length,
		header: ({ column }) => <DataTableColumnHeader column={column} title={m.columnApps()} />,
		cell: ({ row }) => row.original.applicationIds.length,
	},
	{
		id: "updatedAt",
		accessorFn: (row) => row.updatedAt,
		header: ({ column }) => <DataTableColumnHeader column={column} title={m.columnUpdatedAt()} />,
		cell: ({ row }) => formatDate(row.original.updatedAt),
	},
	{
		id: "createdAt",
		accessorFn: (row) => row.createdAt,
		header: ({ column }) => <DataTableColumnHeader column={column} title={m.columnCreatedAt()} />,
		cell: ({ row }) => formatDate(row.original.createdAt),
	},
	{
		id: "actions",
		cell: ({ row }) => {
			async function handleDelete(item: Notification) {
				try {
					await deleteNotification(item.id);
					const identifier = item.name.trim() || item.id;
					toast.success(m.notificationDeleted({ name: identifier }));
				} catch (err) {
					toast.error(err instanceof Error ? err.message : m.failedDeleteNotification());
				}
			}

			async function handleTest(item: Notification) {
				try {
					await testNotification(item.id);
					toast.success(m.testNotificationSent());
				} catch (err) {
					toast.error(err instanceof Error ? err.message : m.failedSendTestNotification());
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
						<DropdownMenuContent align="end" className="w-fit">
							<DropdownMenuLabel>{m.actions()}</DropdownMenuLabel>
							<DropdownMenuItem onClick={() => handleTest(row.original)}>
								<Send className="h-4 w-4" />
								{m.sendTest()}
							</DropdownMenuItem>
							<DropdownMenuSeparator />
							<ConfirmationDialog
								onConfirm={() => {
									void handleDelete(row.original);
								}}
								title={m.deleteNotificationTitle()}
								description={m.deleteNotificationDescription({ name: row.original.name })}
								triggerText={
									<>
										<Trash2 className="h-4 w-4" />
										{m.delete()}
									</>
								}
								asDropdownItem
							/>
						</DropdownMenuContent>
					</DropdownMenu>
				</div>
			);
		},
	},
];
