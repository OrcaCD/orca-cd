import UpsertApplicationDialog from "@/components/dialogs/upsert-application";
import { HealthStatus, SyncStatus, type ApplicationListItem } from "@/lib/applications";
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
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useFetch } from "@/lib/api";

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

function ApplicationsPage() {
	const [viewMode, setViewMode] = useState<"grid" | "list">("grid");
	const [searchQuery, setSearchQuery] = useState("");

	const { data } = useFetch<ApplicationListItem[]>("/applications");

	const filteredApps =
		data?.filter((app) => {
			return (
				app.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
				app.repositoryName.toLowerCase().includes(searchQuery.toLowerCase()) ||
				app.branch.toLowerCase().includes(searchQuery.toLowerCase()) ||
				app.path.toLowerCase().includes(searchQuery.toLowerCase()) ||
				app.agentName.toLowerCase().includes(searchQuery.toLowerCase()) ||
				app.commit.toLowerCase().includes(searchQuery.toLowerCase())
			);
		}) ?? [];

	const stats = {
		total: data?.length ?? 0,
		synced: data?.filter((a) => a.syncStatus === SyncStatus.Synced).length ?? 0,
		outOfSync: data?.filter((a) => a.syncStatus === SyncStatus.OutOfSync).length ?? 0,
		healthy: data?.filter((a) => a.healthStatus === HealthStatus.Healthy).length ?? 0,
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
						<Card key={item.label}>
							<CardHeader>
								<CardTitle>{item.label}</CardTitle>
								<CardContent className={`text-2xl font-bold mt-1 ${item.color ?? ""}`}>
									{item.value}
								</CardContent>
							</CardHeader>
						</Card>
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
					<>
						{data && data.length === 0 ? (
							<div className="rounded-xl border border-dashed p-10 text-center">
								<p className="text-sm font-medium">No applications found</p>
								<p className="mt-1 text-sm text-muted-foreground">
									Adjust your search to see matching applications.
								</p>
							</div>
						) : null}
						<div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
							{filteredApps.map((app) => (
								<Link key={app.id} to="/applications/$id" params={{ id: app.id }}>
									<Card className="border border-border hover:border-primary transition-colors">
										<CardHeader>
											<CardTitle>
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
											</CardTitle>
											<CardAction>
												<DropdownMenu>
													<DropdownMenuTrigger asChild onClick={(e) => e.preventDefault()}>
														<Button variant="ghost" size="icon" className="h-8 w-8">
															<MoreVertical className="h-4 w-4" />
														</Button>
													</DropdownMenuTrigger>
													<DropdownMenuContent align="end">
														<DropdownMenuItem>Sync</DropdownMenuItem>
														<DropdownMenuItem asChild>
															<Link to="/applications/$id/settings" params={{ id: app.id }}>
																Settings
															</Link>
														</DropdownMenuItem>
													</DropdownMenuContent>
												</DropdownMenu>
											</CardAction>
										</CardHeader>
										<CardContent>
											<div className="flex gap-2 mt-4">
												<StatusBadge status={app.syncStatus} type="sync" />
												<StatusBadge status={app.healthStatus} type="health" />
											</div>

											<div className="mt-4 pt-4 border-t border-border space-y-2">
												<div className="flex items-center gap-2 text-sm text-muted-foreground">
													<GitBranch className="h-4 w-4" />
													<span className="truncate">{app.repositoryName}</span>
												</div>
												<div className="flex items-center justify-between text-sm">
													<div className="flex items-center gap-2 text-muted-foreground">
														<GitCommit className="h-4 w-4" />
														<span>{app.commit}</span>
													</div>
													<span className="text-muted-foreground">{app.lastSyncedAt}</span>
												</div>
											</div>
										</CardContent>
									</Card>
								</Link>
							))}
						</div>
					</>
				) : (
					<ApplicationsDataTable columns={columns} data={filteredApps} />
				)}
			</div>
		</div>
	);
}
