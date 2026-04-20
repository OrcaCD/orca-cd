import { createFileRoute } from "@tanstack/react-router";
import { useFetch } from "@/lib/api";
import { type UserDetail } from "@/lib/users";
import UpsertUserDialog from "@/components/dialogs/upsert-user-dialog";
import { UserDataTable } from "@/components/tables/users/data-table";
import { columns } from "@/components/tables/users/columns";
import { m } from "@/lib/paraglide/messages";

export const Route = createFileRoute("/_authenticated/admin/users")({
	component: UsersPage,
	head: () => ({
		meta: [
			{
				title: `${m.admin()} - ${m.adminUserManagement()}`,
			},
		],
	}),
});

function UsersPage() {
	const { data: users, isLoading } = useFetch<UserDetail[]>("/admin/users");

	return (
		<div className="flex flex-col gap-6">
			<div className="flex items-center justify-between">
				<div>
					<h1 className="text-2xl font-bold">{m.adminUserManagement()}</h1>
					<p className="text-muted-foreground text-sm">{m.adminUsersDescription()}</p>
				</div>

				<UpsertUserDialog user={null} />
			</div>

			{isLoading && <p className="text-muted-foreground text-sm">{m.loadingUsers()}</p>}

			<UserDataTable columns={columns} data={users || []} />
		</div>
	);
}
