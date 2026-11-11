import { RelativeTime } from "@/components/relative-time";
import type { ColumnDef } from "@tanstack/react-table";
import { MoreHorizontal, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { DataTableColumnHeader } from "../data-table-column-header";
import { m } from "@/lib/paraglide/messages";
import { usePermissions } from "@/lib/permissions";
import { deleteAgent, type Agent } from "@/lib/agents";
import UpsertAgentDialog from "@/components/dialogs/upsert-agent";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { toast } from "@/components/ui/toast";
import { AgentStatusBadge } from "@/components/badges/agent-status-badge";
import { AgentVersionBadge } from "@/components/badges/agent-version-badge";
import RotateAgentTokenDialog from "@/components/dialogs/rotate-agent-token";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import { dataTableFeatures } from "../table-features";

export const columns: ColumnDef<typeof dataTableFeatures, Agent>[] = [
	{
		accessorKey: "name",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnName()} />;
		},
		cell: ({ row }) => {
			const name = row.original.name;
			const icon = row.original.icon;

			return (
				<div className="flex flex-row gap-3 items-center">
					<div className="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
						<StaticLucideIcon name={icon} className="h-5 w-5 text-primary" />
					</div>

					<div>
						<p className="font-medium">{name}</p>
					</div>
				</div>
			);
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
		accessorKey: "version",
		sortFn: "alphanumeric",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnAgentVersion()} />;
		},
		cell: ({ row }) => {
			const agent = row.original;
			return <AgentVersionBadge version={agent.version} status={agent.versionStatus} />;
		},
	},
	{
		accessorKey: "dockerVersion",
		sortFn: "alphanumeric",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnDockerVersion()} />;
		},
		cell: ({ row }) => {
			const agent = row.original;
			return agent.dockerVersion ?? m.notAvailableShort();
		},
	},
	{
		accessorKey: "lastSeen",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnLastSeen()} />;
		},
		cell: ({ row }) => {
			const agent = row.original;
			return <RelativeTime value={agent.lastSeen} />;
		},
	},
	{
		accessorKey: "createdAt",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnCreatedAt()} />;
		},
		cell: ({ row }) => {
			const agent = row.original;
			return <RelativeTime value={agent.createdAt} />;
		},
	},
	{
		accessorKey: "updatedAt",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnUpdatedAt()} />;
		},
		cell: ({ row }) => {
			const agent = row.original;
			return <RelativeTime value={agent.updatedAt} />;
		},
	},
	{
		id: "actions",
		cell: ({ row }) => <AgentActions agent={row.original} />,
	},
];

function AgentActions({ agent }: { agent: Agent }) {
	const { can, canOnApplicationsOf } = usePermissions();
	const canUpdate = can("agent", "update", agent.id);
	// Its applications are deleted with it
	const canDelete =
		can("agent", "delete", agent.id) && canOnApplicationsOf("delete", agent.appsCount);

	if (!canUpdate && !canDelete) {
		return null;
	}

	async function handleDelete() {
		try {
			await deleteAgent(agent.id);
			const agentIdentifier = agent?.name?.trim() || agent.id;
			toast.add({ type: "success", title: m.agentDeleted({ name: agentIdentifier }) });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedDeleteAgent(),
			});
		}
	}

	return (
		<div className="flex justify-end">
			<DropdownMenu>
				<DropdownMenuTrigger
					render={
						<Button variant="ghost" size="icon" className="h-8 w-8">
							<MoreHorizontal className="h-4 w-4" />
							<span className="sr-only">{m.cardActions()}</span>
						</Button>
					}
				></DropdownMenuTrigger>
				<DropdownMenuContent align="end" className="w-fit" keepMounted>
					<DropdownMenuGroup>
						<DropdownMenuLabel>{m.actions()}</DropdownMenuLabel>
						{canUpdate && (
							<>
								<UpsertAgentDialog agent={agent} asDropdownItem />
								<RotateAgentTokenDialog agent={agent} />
							</>
						)}
						{canUpdate && canDelete && <DropdownMenuSeparator />}
						{canDelete && (
							<ConfirmationDialog
								onConfirm={handleDelete}
								title={m.deleteAgentCardTitle()}
								description={m.deleteAgentCardDescription({ name: agent.name })}
								triggerText={
									<>
										<Trash2 className="h-4 w-4" />
										{m.delete()}
									</>
								}
								asDropdownItem
							/>
						)}
					</DropdownMenuGroup>
				</DropdownMenuContent>
			</DropdownMenu>
		</div>
	);
}
