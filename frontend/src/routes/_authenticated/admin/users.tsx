import { createFileRoute } from "@tanstack/react-router";
import useSWR from "swr";
import fetcher, { API_BASE } from "@/lib/api";
import { type UserDetail } from "@/lib/users";
import UpsertUserDialog from "@/components/dialogs/upsert-user-dialog";
import { UserDataTable } from "@/components/tables/users/data-table";
import { columns } from "@/components/tables/users/columns";

export const Route = createFileRoute("/_authenticated/admin/users")({
	component: UsersPage,
	head: () => ({
		meta: [
			{
				title: "Admin - User Management",
			},
		],
	}),
});

function UsersPage() {
	const { data: users, isLoading } = useSWR<UserDetail[]>(`${API_BASE}/admin/users`, fetcher);

	return (
		<div className="flex flex-col gap-6">
			<div className="flex items-center justify-between">
				<div>
					<h1 className="text-2xl font-bold">User Management</h1>
					<p className="text-muted-foreground text-sm">
						Manage local user accounts and their roles.
					</p>
				</div>

				<UpsertUserDialog user={null} />
			</div>

			{isLoading && <p className="text-muted-foreground text-sm">Loading users...</p>}

			<UserDataTable columns={columns} data={users || []} />
		</div>
	);
}
