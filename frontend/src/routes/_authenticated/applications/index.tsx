import type { ApplicationListItem } from "@/lib/applications";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { z } from "zod";
import { ArrowRight, GitBranch, GitCommit, Search, Server } from "lucide-react";
import { Input } from "@/components/ui/input";
import { ApplicationStatusBadge } from "@/components/badges/application-status-badge";
import { ApplicationsDataTable } from "@/components/tables/applications/data-table";
import { columns } from "@/components/tables/applications/columns";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useFetch } from "@/lib/api";
import { Separator } from "@/components/ui/separator";
import { m } from "@/lib/paraglide/messages";
import { usePreferredLayout } from "@/lib/layout-preference";
import { LayoutToggleGroup } from "@/components/layout-toggle-group";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import UpsertApplicationDialog from "@/components/dialogs/upsert-application";
import {
	fleetCategories,
	fleetCategoryConfig,
	getFleetCategory,
	type FleetCategory,
} from "@/components/applications-overview/fleet";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";

const applicationsSearchSchema = z.object({
	status: z.enum(fleetCategories).optional(),
});

const allStatuses = "all";

export const Route = createFileRoute("/_authenticated/applications/")({
	component: ApplicationsPage,
	// An unknown status (e.g. from an old bookmark) shows the unfiltered list.
	validateSearch: (search) => applicationsSearchSchema.safeParse(search).data ?? {},
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
	const { status } = Route.useSearch();
	const navigate = Route.useNavigate();

	const { data, isLoading } = useFetch<ApplicationListItem[]>("/applications");

	const statusItems: { value: FleetCategory | typeof allStatuses; label: string }[] = [
		{ value: allStatuses, label: m.applicationsFilterAllStatuses() },
		...fleetCategories.map((category) => ({
			value: category,
			label: fleetCategoryConfig[category].label(),
		})),
	];

	const query = searchQuery.toLowerCase();
	const filteredApps =
		data?.filter((app) => {
			if (status && getFleetCategory(app) !== status) {
				return false;
			}
			return (
				app.name.toLowerCase().includes(query) ||
				app.repositoryName.toLowerCase().includes(query) ||
				app.branch.toLowerCase().includes(query) ||
				app.path.toLowerCase().includes(query) ||
				app.agentName.toLowerCase().includes(query) ||
				app.commit.toLowerCase().includes(query)
			);
		}) ?? [];

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
						<Select
							value={status ?? allStatuses}
							onValueChange={(value) => {
								void navigate({
									search: {
										status: fleetCategories.find((category) => category === value),
									},
									replace: true,
								});
							}}
							items={statusItems}
						>
							<SelectTrigger className="w-44" aria-label={m.applicationsFilterStatus()}>
								<SelectValue />
							</SelectTrigger>
							<SelectContent alignItemWithTrigger={false}>
								{statusItems.map((item) => (
									<SelectItem key={item.value} value={item.value}>
										{item.value !== allStatuses ? (
											<span
												className={cn(
													"size-1.5 shrink-0 self-center rounded-full",
													fleetCategoryConfig[item.value].dotClass,
												)}
											/>
										) : null}
										{item.label}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
						<LayoutToggleGroup viewMode={viewMode} setViewMode={setViewMode} />
					</div>
				</div>
			</div>
			<div>
				{viewMode === "grid" ? (
					<>
						{data && filteredApps.length === 0 && !isLoading ? (
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
														<StaticLucideIcon name={app.icon} className="h-5 w-5 text-primary" />
													</div>
													<div>
														<h3 className="font-medium group-hover:text-primary transition-colors">
															{app.name}
														</h3>
													</div>
												</div>
											</CardTitle>
											<CardAction>
												<ArrowRight className="h-5 w-5" />
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
