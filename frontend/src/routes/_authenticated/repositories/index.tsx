import { columns } from "@/components/tables/repositories/columns";
import { RepositoryDataTable } from "@/components/tables/repositories/data-table";
import UpsertRepositoryDialog from "@/components/dialogs/upsert-repository";
import { RepositoryStatus, type Repository } from "@/lib/repsitories";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/repositories/")({
	component: RepositoriesPage,
});

const mockRepos: Repository[] = [
	{
		id: "1",
		name: "org/api-gateway",
		url: "https://github.com/org/api-gateway",
		status: RepositoryStatus.Connected,
		lastSync: new Date(Date.now() - 5 * 60 * 1000), // 5 minutes ago
		apps: 1,
	},
	{
		id: "2",
		name: "org/user-service",
		url: "https://github.com/org/user-service",
		status: RepositoryStatus.Connected,
		lastSync: new Date(Date.now() - 15 * 60 * 1000), // 15 minutes ago
		apps: 1,
	},
	{
		id: "3",
		name: "org/notifications",
		url: "https://github.com/org/notifications",
		status: RepositoryStatus.Connected,
		lastSync: new Date(Date.now() - 60 * 60 * 1000), // 1 hour ago
		apps: 1,
	},
	{
		id: "4",
		name: "org/analytics",
		url: "https://github.com/org/analytics",
		status: RepositoryStatus.Error,
		lastSync: undefined,
		apps: 1,
	},
	{
		id: "5",
		name: "org/payments",
		url: "https://github.com/org/payments",
		status: RepositoryStatus.Connected,
		lastSync: new Date(Date.now() - 5 * 60 * 1000), // 5 minutes ago
		apps: 1,
	},
	{
		id: "6",
		name: "org/frontend",
		url: "https://github.com/org/frontend",
		status: RepositoryStatus.Connected,
		lastSync: new Date(Date.now() - 30 * 60 * 1000), // 30 minutes ago
		apps: 1,
	},
];

function RepositoriesPage() {
	return (
		<div className="p-6 space-y-6">
			<div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
				<div>
					<h1 className="text-2xl font-bold">Repositories</h1>
					<p className="text-muted-foreground text-sm mt-1">Manage connected Git repositories</p>
				</div>
				<UpsertRepositoryDialog repository={null} />
			</div>

			<RepositoryDataTable columns={columns} data={mockRepos} />
		</div>
	);
}
