import type { ColumnDef } from "@tanstack/react-table";
import type { AuditLog } from "@/lib/audit-log";
import { DataTableColumnHeader } from "../data-table-column-header";
import { m } from "@/lib/paraglide/messages";
import { Badge } from "@/components/ui/badge";
import { useAuth } from "@/lib/auth";
import { EventTypeBadge } from "@/components/badges/audit-status-badge";

export const columns: ColumnDef<AuditLog>[] = [
	{
		accessorKey: "createdAt",
		header: ({ column }) => <DataTableColumnHeader column={column} title={m.columnTime()} />,
		cell: ({ row }) => {
			const dateStr = (row.original as any).time || row.original.createdAt;

			if (!dateStr) {
				return <span className="text-muted-foreground">-</span>;
			}

			const date = new Date(dateStr);

			if (isNaN(date.getTime())) {
				return <span className="text-muted-foreground text-xs font-mono">{dateStr}</span>;
			}

			return <span className="text-sm">{date.toLocaleString()}</span>;
		},
	},
	{
		id: "user",
		header: ({ column }) => <DataTableColumnHeader column={column} title={m.columnName()} />,
		cell: ({ row }) => {
			const { auth } = useAuth();
			const user = row.original.user;
			const isSelf = user.id === auth.profile?.id;

			return (
				<>
					<div>
						{user.name}
						{isSelf && (
							<Badge variant="outline" className="ml-2">
								{m.you()}
							</Badge>
						)}
					</div>
					<span className="text-xs text-muted-foreground">{user.email}</span>
				</>
			);
		},
	},
	{
		accessorKey: "eventType",
		header: ({ column }) => <DataTableColumnHeader column={column} title={m.columnEvent()} />,
		cell: ({ row }) => {
			return <EventTypeBadge type={row.original.eventType} />;
		},
	},
	{
		accessorKey: "targetType",
		header: ({ column }) => <DataTableColumnHeader column={column} title={m.columnTargetType()} />,
		cell: ({ row }) => <span className="capitalize font-medium">{row.original.targetType}</span>,
	},
	{
		accessorKey: "targetId",
		header: ({ column }) => <DataTableColumnHeader column={column} title={m.columnTargetId()} />,
		cell: ({ row }) => {
			const id = row.original.targetId;
			if (!id) {
				return <span className="text-muted-foreground">-</span>;
			}

			return (
				<span className="font-mono text-xs text-muted-foreground whitespace-nowrap select-all">
					{id}
				</span>
			);
		},
	},
];
