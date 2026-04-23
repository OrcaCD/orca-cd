import type { ColumnDef } from "@tanstack/react-table";
import { MoreHorizontal, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuLabel,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { DataTableColumnHeader } from "../data-table-column-header";
import { m } from "@/lib/paraglide/messages";
import { deleteAgent, type Agent } from "@/lib/agents";
import { AgentStatusBadge } from "@/components/cards/agents/data-cards";
import UpsertAgentDialog from "@/components/dialogs/upsert-agent";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { toast } from "sonner";

export const columns: ColumnDef<Agent>[] = [
	{
		accessorKey: "name",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnName()} />;
		},
	},
	{
		accessorKey: "status",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnStatus()} />;
		},
		cell: ({ row }) => {
			const agent = row.original;
			return <AgentStatusBadge status={agent.status} />;
		},
	},
	{
		accessorKey: "appsCount",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnAppsCount()} />;
		},
		cell: ({ row }) => {
			const agent = row.original;
			return agent.appsCount ?? 0;
		},
	},
	{
		accessorKey: "lastSeen",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnLastSeen()} />;
		},
		cell: ({ row }) => {
			const agent = row.original;
			return agent.lastSeen ? new Date(agent.lastSeen).toLocaleString() : m.never();
		},
	},
	{
		accessorKey: "createdAt",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnCreatedAt()} />;
		},
		cell: ({ row }) => {
			const agent = row.original;
			return agent.createdAt ? new Date(agent.createdAt).toLocaleString() : m.never();
		},
	},
	{
		accessorKey: "updatedAt",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnUpdatedAt()} />;
		},
		cell: ({ row }) => {
			const agent = row.original;
			return agent.updatedAt ? new Date(agent.updatedAt).toLocaleString() : m.never();
		},
	},
	{
		id: "actions",
		cell: ({ row }) => {
			async function handleDeleteCard(agent: Agent) {
				try {
					await deleteAgent(agent.id);
					const agentIdentifier = agent?.name?.trim() || agent.id;
					toast.success(m.agentDeleted({ name: agentIdentifier }));
				} catch (err) {
					toast.error(err instanceof Error ? err.message : m.failedDeleteAgent());
				}
			}
			return (
				<div className="flex justify-end">
					<DropdownMenu>
						<DropdownMenuTrigger asChild>
							<Button variant="ghost" size="icon" className="h-8 w-8">
								<MoreHorizontal className="h-4 w-4" />
								<span className="sr-only">{m.cardActions()}</span>
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end">
							<DropdownMenuLabel>{m.actions()}</DropdownMenuLabel>
							<UpsertAgentDialog agent={row.original} asDropdownItem />
							<ConfirmationDialog
								onConfirm={() => handleDeleteCard(row.original)}
								title={m.deleteAgentCardTitle()}
								description={m.deleteAgentCardDescription({ name: row.original.name })}
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
