import type { ColumnDef } from "@tanstack/react-table";
import { EllipsisVertical, KeyRound, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	DropdownMenuGroup,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { DialogDropdownMenu, DialogDropdownMenuContent } from "@/components/dialog-dropdown-menu";
import { DataTableColumnHeader } from "../data-table-column-header";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { toast } from "@/components/ui/toast";
import { deleteUser, type UserDetail } from "@/lib/users";
import { useAuth } from "@/lib/auth";
import { Badge } from "@/components/ui/badge";
import UpsertUserDialog from "@/components/dialogs/upsert-user-dialog";
import { m } from "@/lib/paraglide/messages";
import { dataTableFeatures } from "../table-features";
import { RoleBadges } from "@/components/badges/role-badge";
import { roleName } from "@/lib/roles";

export const columns: ColumnDef<typeof dataTableFeatures, UserDetail>[] = [
	{
		accessorKey: "name",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnName()} />;
		},
		cell: ({ row }) => {
			const { auth } = useAuth();
			const user = row.original;
			const isSelf = user.id === auth.profile?.id;

			return (
				<>
					{user.name}
					{isSelf && (
						<Badge variant="outline" className="ml-2">
							{m.you()}
						</Badge>
					)}
				</>
			);
		},
	},
	{
		accessorKey: "email",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnEmail()} />;
		},
	},
	{
		id: "roles",
		accessorFn: (user) => user.roles.map((role) => roleName(role)).join(", "),
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnRoles()} />;
		},
		cell: ({ row }) => <RoleBadges roles={row.original.roles} />,
	},
	{
		accessorKey: "providers",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnProviders()} />;
		},
		cell: ({ row }) => {
			const providers = row.original.providers;
			return (
				<div className="flex flex-wrap gap-1 max-w-sm">
					{providers.map((provider) => (
						<Badge key={provider} variant="outline">
							{provider === "password" && <KeyRound className="mr-1 h-3 w-3" />}
							{provider === "password" ? m.providerPassword() : provider}
						</Badge>
					))}
				</div>
			);
		},
	},
	{
		id: "actions",
		cell: ({ row }) => {
			async function handleDelete() {
				try {
					await deleteUser(row.original.id);
					toast.add({ type: "success", title: m.userDeleted() });
				} catch (err) {
					toast.add({
						type: "error",
						title: err instanceof Error ? err.message : m.failedDeleteUser(),
					});
				}
			}

			return (
				<div className="flex justify-end">
					<DialogDropdownMenu>
						<DropdownMenuTrigger
							render={
								<Button variant="ghost" size="icon" className="h-8 w-8">
									<EllipsisVertical className="h-4 w-4" />
									<span className="sr-only">{m.actions()}</span>
								</Button>
							}
						></DropdownMenuTrigger>
						<DialogDropdownMenuContent align="end">
							<DropdownMenuGroup>
								<UpsertUserDialog user={row.original} asDropdownItem />
								<DropdownMenuSeparator />
								<ConfirmationDialog
									onConfirm={() => handleDelete()}
									title={m.deleteUserTitle()}
									description={m.deleteUserDescription({
										name: row.original.name,
										email: row.original.email,
									})}
									triggerText={
										<>
											<Trash2 className="h-4 w-4" />
											{m.delete()}
										</>
									}
									asDropdownItem
								/>
							</DropdownMenuGroup>
						</DialogDropdownMenuContent>
					</DialogDropdownMenu>
				</div>
			);
		},
	},
];
