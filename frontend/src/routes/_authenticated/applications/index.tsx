import { ApplicationView } from "@/components/application/view/application-view";
import UpsertApplicationDialog from "@/components/dialogs/upsert-application";
import { ApplicationStats } from "@/components/application/view/application-stats";
import { ApplicationFilters } from "@/components/application/view/application-toolbar/application-filters";
import { HealthStatus, SyncStatus, type Application } from "@/lib/applications";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";

export const Route = createFileRoute("/_authenticated/applications/")({
	component: ApplicationsPage,
	head: () => ({
		meta: [
			{
				title: "Applications",
			},
		],
	}),
});

const mockApps: Application[] = [
	{
		id: "1",
		name: "api-gateway",
		project: "production",
		syncStatus: SyncStatus.Synced,
		healthStatus: HealthStatus.Healthy,
		repo: "org/api-gateway",
		branch: "main",
		commit: "a3f2b1c",
		lastSync: "2m ago",
		containers: 3,
	},
	{
		id: "2",
		name: "user-service",
		project: "production",
		syncStatus: SyncStatus.OutOfSync,
		healthStatus: HealthStatus.Healthy,
		repo: "org/user-service",
		branch: "main",
		commit: "b4e5d2f",
		lastSync: "15m ago",
		containers: 2,
	},
	{
		id: "3",
		name: "notification-service",
		project: "production",
		syncStatus: SyncStatus.Progressing,
		healthStatus: HealthStatus.Progressing,
		repo: "org/notifications",
		branch: "main",
		commit: "c5f6e3g",
		lastSync: "1m ago",
		containers: 1,
	},
	{
		id: "4",
		name: "analytics-dashboard",
		project: "staging",
		syncStatus: SyncStatus.Synced,
		healthStatus: HealthStatus.Healthy,
		repo: "org/analytics",
		branch: "develop",
		commit: "d6g7f4h",
		lastSync: "1h ago",
		containers: 4,
	},
	{
		id: "5",
		name: "payment-processor",
		project: "production",
		syncStatus: SyncStatus.Synced,
		healthStatus: HealthStatus.Degraded,
		repo: "org/payments",
		branch: "main",
		commit: "e7h8g5i",
		lastSync: "5m ago",
		containers: 2,
	},
	{
		id: "6",
		name: "frontend-app",
		project: "staging",
		syncStatus: SyncStatus.Synced,
		healthStatus: HealthStatus.Healthy,
		repo: "org/frontend",
		branch: "feature/new-ui",
		commit: "f8i9h6j",
		lastSync: "30m ago",
		containers: 1,
	},
]

function ApplicationsPage() {
	const [viewMode, setViewMode] = useState<"grid" | "list">("grid")
	const [searchQuery, setSearchQuery] = useState("")
	const [projectFilter, setProjectFilter] = useState<string>("all")

	const filteredApps = mockApps.filter((app) => {
		const matchesSearch = app.name.toLowerCase().includes(searchQuery.toLowerCase())
		const matchesProject = projectFilter === "all" || app.project === projectFilter
		return matchesSearch && matchesProject
	})
	return (
		<div className="p-6 space-y-6">
			<div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
				<div>
					<h1 className="text-2xl font-bold">Applications</h1>
					<p className="text-muted-foreground text-sm mt-1">Manage and monitor your Docker deployments</p>
				</div>
				<UpsertApplicationDialog application={null} />
			</div>
			<div>
				<ApplicationStats apps={mockApps} />
			</div>
			<div>
				<ApplicationFilters viewMode={viewMode} setViewMode={setViewMode} searchQuery={searchQuery} setSearchQuery={setSearchQuery} projectFilter={projectFilter} setProjectFilter={setProjectFilter} />
			</div>
			<div>
				<ApplicationView viewMode={viewMode} apps={filteredApps} />
			</div>
		</div>
	);
}
