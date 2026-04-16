import UpsertApplicationDialog from "@/components/dialogs/upsert-application";
import { HealthStatus, SyncStatus, type Application } from "@/lib/applications";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
	Box,
	GitBranch,
	GitCommit,
	LayoutGrid,
	List,
	MoreVertical,
	RefreshCw,
	Search,
} from "lucide-react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { StatusBadge } from "@/components/status-badge";
import {
	DropdownMenu,
	DropdownMenuTrigger,
	DropdownMenuContent,
	DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import { ApplicationsDataTable } from "@/components/tables/applications/data-table";
import { columns } from "@/components/tables/applications/columns";

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
		syncStatus: SyncStatus.Synced,
		healthStatus: HealthStatus.Healthy,
		repo: "org/api-gateway",
		branch: "main",
		commit: "a3f2b1c",
		commitMessage: "Initial commit",
		lastSync: "2m ago",
		path: "apps/api-gateway",
		agent: "agent-01",
	},
	{
		id: "2",
		name: "user-service",
		syncStatus: SyncStatus.OutOfSync,
		healthStatus: HealthStatus.Healthy,
		repo: "org/user-service",
		branch: "main",
		commit: "b4e5d2f",
		commitMessage: "Fix user authentication bug",
		lastSync: "15m ago",
		agent: "agent-02",
		path: "apps/user-service",
	},
	{
		id: "3",
		name: "notification-service",
		syncStatus: SyncStatus.Progressing,
		healthStatus: HealthStatus.Progressing,
		repo: "org/notifications",
		branch: "main",
		commit: "c5f6e3g",
		commitMessage: "Add email notification support",
		lastSync: "1m ago",
		agent: "agent-01",
		path: "apps/notification-service",
	},
	{
		id: "4",
		name: "analytics-dashboard",
		syncStatus: SyncStatus.Synced,
		healthStatus: HealthStatus.Healthy,
		repo: "org/analytics",
		branch: "develop",
		commit: "d6g7f4h",
		commitMessage: "Update dashboard layout",
		lastSync: "1h ago",
		agent: "agent-03",
		path: "apps/analytics-dashboard",
	},
	{
		id: "5",
		name: "payment-processor",
		syncStatus: SyncStatus.Synced,
		healthStatus: HealthStatus.Degraded,
		repo: "org/payments",
		branch: "main",
		commit: "e7h8g5i",
		commitMessage: "Fix payment processing bug",
		lastSync: "5m ago",
		agent: "agent-02",
		path: "apps/payment-processor",
	},
	{
		id: "6",
		name: "frontend-app",
		syncStatus: SyncStatus.Synced,
		healthStatus: HealthStatus.Healthy,
		repo: "org/frontend",
		branch: "feature/new-ui",
		commit: "f8i9h6j",
		commitMessage: "Add new UI components",
		lastSync: "30m ago",
		agent: "agent-03",
		path: "apps/frontend",
	},
];

function ApplicationsPage() {
	const [viewMode, setViewMode] = useState<"grid" | "list">("grid");
	const [searchQuery, setSearchQuery] = useState("");

	const filteredApps = mockApps.filter((app) => {
		return (
			app.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
			app.repo.toLowerCase().includes(searchQuery.toLowerCase()) ||
			app.branch.toLowerCase().includes(searchQuery.toLowerCase()) ||
			app.path.toLowerCase().includes(searchQuery.toLowerCase()) ||
			app.agent.toLowerCase().includes(searchQuery.toLowerCase()) ||
			app.commit.toLowerCase().includes(searchQuery.toLowerCase())
		);
	});

	const stats = {
		total: mockApps.length,
		synced: mockApps.filter((a) => a.syncStatus === SyncStatus.Synced).length,
		outOfSync: mockApps.filter((a) => a.syncStatus === SyncStatus.OutOfSync).length,
		healthy: mockApps.filter((a) => a.healthStatus === HealthStatus.Healthy).length,
	};
	const statItems = [
		{ label: "Total Apps", value: stats.total },
		{ label: "Synced", value: stats.synced, color: "text-emerald-400" },
		{ label: "Out of Sync", value: stats.outOfSync, color: "text-amber-400" },
		{ label: "Healthy", value: stats.healthy, color: "text-emerald-400" },
	];
	return (
		<div className="p-6 space-y-6">
			<div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
				<div>
					<h1 className="text-2xl font-bold">Applications</h1>
					<p className="text-muted-foreground text-sm mt-1">
						Manage and monitor your Docker deployments
					</p>
				</div>
				<UpsertApplicationDialog application={null} />
			</div>
			<div>
				<div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
					{statItems.map((item) => (
						<div key={item.label} className="bg-card border border-border rounded-lg p-4">
							<p className="text-muted-foreground text-sm">{item.label}</p>
							<p className={`text-2xl font-bold mt-1 ${item.color ?? ""}`}>{item.value}</p>
						</div>
					))}
				</div>
			</div>
			<div>
				<div className="flex flex-col sm:flex-row gap-4">
					<div className="relative flex-1">
						<Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
						<Input
							placeholder="Search applications..."
							className="pl-9 bg-muted border-border"
							value={searchQuery}
							onChange={(e) => setSearchQuery(e.target.value)}
						/>
					</div>

					<div className="flex gap-2">
						<Button variant="outline" size="icon">
							<RefreshCw className="h-4 w-4" />
						</Button>

						<div className="flex border border-border rounded-md">
							<Button
								variant="ghost"
								size="icon"
								className={cn(viewMode === "grid" && "bg-muted")}
								onClick={() => setViewMode("grid")}
							>
								<LayoutGrid className="h-4 w-4" />
							</Button>

							<Button
								variant="ghost"
								size="icon"
								className={cn(viewMode === "list" && "bg-muted")}
								onClick={() => setViewMode("list")}
							>
								<List className="h-4 w-4" />
							</Button>
						</div>
					</div>
				</div>
			</div>
			<div>
				{viewMode === "grid" ? (
					<div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
						{filteredApps.map((app) => (
							<Link
								key={app.id}
								to="/applications/$id"
								params={{ id: app.id }}
								className="group bg-card border border-border rounded-lg p-4 hover:border-primary transition-colors"
							>
								<div className="flex items-start justify-between">
									<div className="flex items-center gap-3">
										<div className="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
											<Box className="h-5 w-5 text-primary" />
										</div>
										<div>
											<h3 className="font-medium group-hover:text-primary transition-colors">
												{app.name}
											</h3>
										</div>
									</div>
									<DropdownMenu>
										<DropdownMenuTrigger asChild onClick={(e) => e.preventDefault()}>
											<Button variant="ghost" size="icon" className="h-8 w-8">
												<MoreVertical className="h-4 w-4" />
											</Button>
										</DropdownMenuTrigger>
										<DropdownMenuContent align="end">
											<DropdownMenuItem>Sync</DropdownMenuItem>
											<DropdownMenuItem>Refresh</DropdownMenuItem>
											<DropdownMenuItem>Settings</DropdownMenuItem>
											<DropdownMenuItem className="text-destructive">Delete</DropdownMenuItem>
										</DropdownMenuContent>
									</DropdownMenu>
								</div>

								<div className="flex gap-2 mt-4">
									<StatusBadge status={app.syncStatus} />
									<StatusBadge status={app.healthStatus} />
								</div>

								<div className="mt-4 pt-4 border-t border-border space-y-2">
									<div className="flex items-center gap-2 text-sm text-muted-foreground">
										<GitBranch className="h-4 w-4" />
										<span className="truncate">{app.repo}</span>
									</div>
									<div className="flex items-center justify-between text-sm">
										<div className="flex items-center gap-2 text-muted-foreground">
											<GitCommit className="h-4 w-4" />
											<span>{app.commit}</span>
										</div>
										<span className="text-muted-foreground">{app.lastSync}</span>
									</div>
								</div>
							</Link>
						))}
					</div>
				) : (
					<ApplicationsDataTable columns={columns} data={filteredApps} />
				)}
			</div>
		</div>
	);
}
