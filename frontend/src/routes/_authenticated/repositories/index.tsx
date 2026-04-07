import { columns } from "@/components/tables/repositories/columns";
import { RepositoryDataTable } from "@/components/tables/repositories/data-table";
import UpsertRepositoryDialog from "@/components/dialogs/upsert-repository";
import type { Repository } from "@/lib/repsitories";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_authenticated/repositories/")({
	component: RepositoriesPage,
});

const mockRepos: Repository[] = [
	{
		id: "1",
		name: "org/api-gateway",
		url: "https://github.com/org/api-gateway",
		provider: "github",
		authMethod: "token",
		syncType: "polling",
		syncStatus: "success",
		lastSyncError: null,
		pollingIntervalSeconds: 300,
		lastSyncedAt: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
		createdBy: "admin",
		createdAt: new Date().toISOString(),
		updatedAt: new Date().toISOString(),
		apps: 0,
	},
	{
		id: "2",
		name: "org/user-service",
		url: "https://github.com/org/user-service",
		provider: "github",
		authMethod: "token",
		syncType: "polling",
		syncStatus: "success",
		lastSyncError: null,
		pollingIntervalSeconds: 300,
		lastSyncedAt: new Date(Date.now() - 15 * 60 * 1000).toISOString(),
		createdBy: "admin",
		createdAt: new Date().toISOString(),
		updatedAt: new Date().toISOString(),
		apps: 0,
	},
	{
		id: "3",
		name: "org/notifications",
		url: "https://github.com/org/notifications",
		provider: "github",
		authMethod: "token",
		syncType: "polling",
		syncStatus: "success",
		lastSyncError: null,
		pollingIntervalSeconds: 300,
		lastSyncedAt: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
		createdBy: "admin",
		createdAt: new Date().toISOString(),
		updatedAt: new Date().toISOString(),
		apps: 0,
	},
	{
		id: "4",
		name: "org/analytics",
		url: "https://github.com/org/analytics",
		provider: "github",
		authMethod: "token",
		syncType: "polling",
		syncStatus: "failed",
		lastSyncError: "Authentication failed",
		pollingIntervalSeconds: 300,
		lastSyncedAt: null,
		createdBy: "admin",
		createdAt: new Date().toISOString(),
		updatedAt: new Date().toISOString(),
		apps: 0,
	},
	{
		id: "5",
		name: "org/payments",
		url: "https://github.com/org/payments",
		provider: "github",
		authMethod: "token",
		syncType: "webhook",
		syncStatus: "success",
		lastSyncError: null,
		pollingIntervalSeconds: null,
		lastSyncedAt: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
		createdBy: "admin",
		createdAt: new Date().toISOString(),
		updatedAt: new Date().toISOString(),
		apps: 0,
	},
	{
		id: "6",
		name: "org/frontend",
		url: "https://github.com/org/frontend",
		provider: "github",
		authMethod: "token",
		syncType: "polling",
		syncStatus: "success",
		lastSyncError: null,
		pollingIntervalSeconds: 300,
		lastSyncedAt: new Date(Date.now() - 30 * 60 * 1000).toISOString(),
		createdBy: "admin",
		createdAt: new Date().toISOString(),
		updatedAt: new Date().toISOString(),
		apps: 0,
	},
];

export default function RepositoriesPage() {
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
