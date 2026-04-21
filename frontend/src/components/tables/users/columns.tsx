import type { ColumnDef } from "@tanstack/react-table";
import { EllipsisVertical, KeyRound, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { DataTableColumnHeader } from "../data-table-column-header";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { toast } from "sonner";
import { deleteUser, type UserDetail } from "@/lib/users";
import { useAuth } from "@/lib/auth";
import { Badge } from "@/components/ui/badge";
import UpsertUserDialog from "@/components/dialogs/upsert-user-dialog";
import { m } from "@/lib/paraglide/messages";

export const columns: ColumnDef<UserDetail>[] = [
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
		accessorKey: "role",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title={m.columnRole()} />;
		},
		cell: ({ row }) => {
			const role = row.original.role;
			return (
				<Badge variant={role === "admin" ? "default" : "secondary"}>
					{role === "admin" ? m.roleAdmin() : role === "user" ? m.roleUser() : role}
				</Badge>
			);
		},
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
			const hasPasswordProvider = row.original.providers.includes("password");

			async function handleDelete() {
				try {
					await deleteUser(row.original.id);
					toast.success(m.userDeleted());
				} catch (err) {
					toast.error(err instanceof Error ? err.message : m.failedDeleteUser());
				}
			}

			return (
				<div className="flex justify-end">
					<DropdownMenu>
						<DropdownMenuTrigger asChild>
							<Button variant="ghost" size="icon" className="h-8 w-8">
								<EllipsisVertical className="h-4 w-4" />
								<span className="sr-only">{m.actions()}</span>
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end">
							<UpsertUserDialog
								user={row.original}
								asDropdownItem
								disabled={!hasPasswordProvider}
							/>
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
						</DropdownMenuContent>
					</DropdownMenu>
				</div>
			);
		},
	},
];
