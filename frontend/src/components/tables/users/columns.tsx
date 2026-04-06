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

export const columns: ColumnDef<UserDetail>[] = [
	{
		accessorKey: "name",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Name" />;
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
							You
						</Badge>
					)}
				</>
			);
		},
	},
	{
		accessorKey: "email",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Email" />;
		},
	},
	{
		accessorKey: "role",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Role" />;
		},
		cell: ({ row }) => {
			const role = row.original.role;
			return <Badge variant={role === "admin" ? "default" : "secondary"}>{role}</Badge>;
		},
	},
	{
		accessorKey: "hasPassword",
		header: ({ column }) => {
			return <DataTableColumnHeader column={column} title="Has Password" />;
		},
		cell: ({ row }) => {
			const hasPassword = row.original.hasPassword;
			return (
				<>
					{hasPassword && (
						<Badge variant="outline">
							<KeyRound className="mr-1 h-3 w-3" />
							Password
						</Badge>
					)}
				</>
			);
		},
	},
	{
		id: "actions",
		cell: ({ row }) => {
			async function handleDelete() {
				try {
					await deleteUser(row.original.id);
					toast.success("User deleted");
				} catch (err) {
					toast.error(err instanceof Error ? err.message : "Failed to delete user");
				}
			}

			return (
				<div className="flex justify-end">
					<DropdownMenu>
						<DropdownMenuTrigger asChild>
							<Button variant="ghost" size="icon" className="h-8 w-8">
								<EllipsisVertical className="h-4 w-4" />
								<span className="sr-only">Actions</span>
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end">
							<UpsertUserDialog user={row.original} asDropdownItem />
							<DropdownMenuSeparator />
							<ConfirmationDialog
								onConfirm={() => handleDelete()}
								title="Delete user?"
								description={`This will permanently delete "${row.original.name}" (${row.original.email}). This action cannot be undone.`}
								triggerText={
									<>
										<Trash2 className="h-4 w-4" />
										Delete
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
