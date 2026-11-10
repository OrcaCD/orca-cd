import type { ApplicationListItem } from "@/lib/applications";
import type { Agent } from "@/lib/agents";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { z } from "zod";
import { Search } from "lucide-react";
import { Input } from "@/components/ui/input";
import { ApplicationsDataTable } from "@/components/tables/applications/data-table";
import { columns } from "@/components/tables/applications/columns";
import { ApplicationCard } from "@/components/cards/application-card";
import { useFetch } from "@/lib/api";
import { m } from "@/lib/paraglide/messages";
import { usePreferredLayout } from "@/lib/layout-preference";
import { LayoutToggleGroup } from "@/components/layout-toggle-group";
import UpsertApplicationDialog from "@/components/dialogs/upsert-application";
import {
	fleetCategories,
	fleetCategoryConfig,
	getFleetCategory,
	type FleetCategory,
} from "@/lib/fleet";
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
	agent: z.string().optional(),
});

const allStatuses = "all";
const allAgents = "all";

export const Route = createFileRoute("/_authenticated/applications/")({
	component: ApplicationsPage,
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
	const { status, agent } = Route.useSearch();
	const navigate = Route.useNavigate();

	const { data, isLoading } = useFetch<ApplicationListItem[]>("/applications");
	const { data: agents } = useFetch<Agent[]>("/agents");

	const statusItems: { value: FleetCategory | typeof allStatuses; label: string }[] = [
		{ value: allStatuses, label: m.applicationsFilterAllStatuses() },
		...fleetCategories.map((category) => ({
			value: category,
			label: fleetCategoryConfig[category].label(),
		})),
	];
	const agentItems = [
		{ value: allAgents, label: m.applicationsFilterAllAgents() },
		...(agents ?? []).map((item) => ({ value: item.id, label: item.name })),
	];

	const query = searchQuery.toLowerCase();
	const filteredApps =
		data?.filter((app) => {
			if (status && getFleetCategory(app) !== status) {
				return false;
			}
			if (agent && app.agentId !== agent) {
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
									search: (prev) => ({
										...prev,
										status: fleetCategories.find((category) => category === value),
									}),
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
						{agents && (agents.length > 1 || agent) ? (
							<Select
								value={agent ?? allAgents}
								onValueChange={(value) => {
									void navigate({
										search: (prev) => ({
											...prev,
											agent: value === allAgents ? undefined : (value ?? undefined),
										}),
										replace: true,
									});
								}}
								items={agentItems}
							>
								<SelectTrigger className="w-44" aria-label={m.applicationsFilterAgent()}>
									<SelectValue />
								</SelectTrigger>
								<SelectContent alignItemWithTrigger={false}>
									{agentItems.map((item) => (
										<SelectItem key={item.value} value={item.value}>
											{item.label}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						) : null}
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
								<ApplicationCard key={app.id} app={app} />
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
