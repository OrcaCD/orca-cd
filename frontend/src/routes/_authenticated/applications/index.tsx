import UpsertApplicationDialog from "@/components/dialogs/upsert-application";
import { HealthStatus, SyncStatus, type ApplicationListItem } from "@/lib/applications";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
	ArrowRight,
	Box,
	GitBranch,
	GitCommit,
	LayoutGrid,
	List,
	MoreVertical,
	RefreshCw,
	Search,
	Server,
} from "lucide-react";
import { Input } from "@/components/ui/input";
import { ApplicationStatusBadge } from "@/components/badges/application-status-badge";
import {
	DropdownMenu,
	DropdownMenuTrigger,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import { ApplicationsDataTable } from "@/components/tables/applications/data-table";
import { columns } from "@/components/tables/applications/columns";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useFetch } from "@/lib/api";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Separator } from "@/components/ui/separator";
import { m } from "@/lib/paraglide/messages";
import { toPreferredLayout, usePreferredLayout } from "@/lib/layout-preference";

export const Route = createFileRoute("/_authenticated/applications/")({
	component: ApplicationsPage,
	head: () => ({
		meta: [
			{
				title: m.pageApplications(),
			},
		],
	}),
});

function ApplicationsPage() {
	const { preferredLayout: viewMode, setPreferredLayout: setViewMode } = usePreferredLayout();
	const [searchQuery, setSearchQuery] = useState("");

	const { data, isLoading } = useFetch<ApplicationListItem[]>("/applications");

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
		{ label: m.applicationsStatTotalApps(), value: stats.total },
		{ label: m.applicationsStatSynced(), value: stats.synced, color: "text-emerald-400" },
		{ label: m.applicationsStatOutOfSync(), value: stats.outOfSync, color: "text-amber-400" },
		{ label: m.applicationsStatHealthy(), value: stats.healthy, color: "text-emerald-400" },
	];
	return (
		<div className="p-6 space-y-6">
			<div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
				<div>
					<h1 className="text-2xl font-bold">{m.pageApplications()}</h1>
					<p className="text-muted-foreground text-sm mt-1">{m.applicationsPageDescription()}</p>
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
							placeholder={m.searchApplications()}
							className="pl-9 bg-muted border-border"
							value={searchQuery}
							onChange={(e) => setSearchQuery(e.target.value)}
						/>
					</div>

					<div className="flex gap-2">
						<ToggleGroup
							type="single"
							variant="outline"
							value={viewMode}
							onValueChange={(value) => {
								const nextLayout = toPreferredLayout(value);
								if (nextLayout) {
									setViewMode(nextLayout);
								}
							}}
						>
							<ToggleGroupItem value="grid">
								<LayoutGrid className="h-4 w-4" />
							</ToggleGroupItem>

							<ToggleGroupItem value="table">
								<List className="h-4 w-4" />
							</ToggleGroupItem>
						</ToggleGroup>
					</div>
				</div>
			</div>
			<div>
				{viewMode === "grid" ? (
					<>
						{data && data.length === 0 && !isLoading ? (
							<div className="rounded-xl border border-dashed p-10 text-center">
								<p className="text-sm font-medium">{m.noApplicationsFound()}</p>
								<p className="mt-1 text-sm text-muted-foreground">
									{m.noApplicationsFoundDescription()}
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
													<DropdownMenuContent align="end" className="w-full">
														<DropdownMenuItem asChild>
															<Link to="/applications/$id" params={{ id: app.id }}>
																<ArrowRight className="mr-2 h-4 w-4" />
																{m.details()}
															</Link>
														</DropdownMenuItem>
														<DropdownMenuSeparator />
														<DropdownMenuItem>
															<RefreshCw className="mr-2 h-4 w-4" />
															{m.sync()}
														</DropdownMenuItem>
													</DropdownMenuContent>
												</DropdownMenu>
											</CardAction>
										</CardHeader>
										<CardContent>
											<div className="flex gap-2">
												<ApplicationStatusBadge status={app.syncStatus} type="sync" />
												<ApplicationStatusBadge status={app.healthStatus} type="health" />
											</div>

											<Separator className="my-4" />

											<div className="space-y-2">
												<div className="flex items-center gap-2 text-sm text-muted-foreground">
													<Server className="h-4 w-4" />
													<span className="truncate">{app.agentName}</span>
												</div>
												<div className="flex items-center gap-2 text-sm text-muted-foreground">
													<GitBranch className="h-4 w-4" />
													<span className="truncate">
														{app.repositoryName} @ {app.branch}
													</span>
												</div>
												<div className="flex items-center justify-between text-sm">
													<div className="flex items-center gap-2 text-muted-foreground">
														<GitCommit className="h-4 w-4" />
														<span>{app.commit.slice(0, 7)}</span>
													</div>
													<span className="text-muted-foreground">
														{app.lastSyncedAt
															? new Date(app.lastSyncedAt).toLocaleString()
															: m.never()}
													</span>
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
