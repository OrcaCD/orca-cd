import type { ApplicationListItem } from "@/lib/applications";
import { PageContainer, PageHeader } from "@/components/page-header";
import {
	EmptyState,
	ResourceGridSkeleton,
	ResourceTableSkeleton,
} from "@/components/collection-states";
import { Button } from "@/components/ui/button";
import { Container, Plus } from "lucide-react";
import { SearchField } from "@/components/search-field";
import type { Agent } from "@/lib/agents";
import { createFileRoute } from "@tanstack/react-router";
import { useRef, useState } from "react";
import { z } from "zod";
import { ApplicationsDataTable } from "@/components/tables/applications/data-table";
import { columns } from "@/components/tables/applications/columns";
import { ApplicationCard } from "@/components/cards/application-card";
import { useFetch } from "@/lib/api";
import { m } from "@/lib/paraglide/messages";
import { usePermissions } from "@/lib/permissions";
import { usePreferredLayout } from "@/lib/layout-preference";
import { LayoutToggleGroup } from "@/components/layout-toggle-group";
import UpsertApplicationDialog from "@/components/dialogs/upsert-application";
import { useSharedDialog } from "@/hooks/use-shared-dialog";
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
	const searchInputRef = useRef<HTMLInputElement>(null);
	// The list replaces the empty state as soon as an application exists, the dialog has to stay.
	const {
		fallbackRef: addApplicationRef,
		triggerProps: addApplicationTrigger,
		openFrom: openAddApplication,
		dialogProps: addApplicationDialog,
	} = useSharedDialog();
	const { can } = usePermissions();
	const canCreate = can("application", "create");
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

	const hasFilters = Boolean(status || agent);
	const clearFilters = () => {
		setSearchQuery("");
		void navigate({ search: {}, replace: true });
		searchInputRef.current?.focus();
	};

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
		<PageContainer>
			<PageHeader
				title={m.pageApplications()}
				description={m.applicationsPageDescription()}
				actions={
					canCreate && (
						<Button {...addApplicationTrigger} ref={addApplicationRef} onClick={openAddApplication}>
							<Plus />
							{m.newApplication()}
						</Button>
					)
				}
			/>
			{canCreate && <UpsertApplicationDialog application={null} {...addApplicationDialog} />}
			{(data?.length !== 0 || hasFilters || searchQuery.length > 0) && (
				<div>
					<div className="flex flex-col gap-3 sm:flex-row">
						<SearchField
							value={searchQuery}
							onChange={setSearchQuery}
							inputRef={searchInputRef}
							label={m.searchApplicationsLabel()}
							placeholder={m.searchApplications()}
							className="flex-1"
						/>

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
			)}
			{isLoading ? (
				viewMode === "grid" ? (
					<ResourceGridSkeleton
						label={m.loadingApplications()}
						className="md:grid-cols-2 xl:grid-cols-3"
					/>
				) : (
					<ResourceTableSkeleton label={m.loadingApplications()} />
				)
			) : data?.length === 0 ? (
				<EmptyState
					icon={Container}
					title={m.noApplicationsYet()}
					description={m.noApplicationsYetDescription()}
					action={
						canCreate && (
							<Button {...addApplicationTrigger} onClick={openAddApplication}>
								<Plus />
								{m.newApplication()}
							</Button>
						)
					}
				/>
			) : data && filteredApps.length === 0 ? (
				<EmptyState
					variant="noMatches"
					title={m.noApplicationsFound()}
					description={
						hasFilters
							? m.noApplicationsFoundFilteredDescription()
							: m.noApplicationsFoundDescription()
					}
					action={
						<Button variant="outline" onClick={clearFilters}>
							{hasFilters ? m.filtersClear() : m.searchClear()}
						</Button>
					}
				/>
			) : viewMode === "grid" ? (
				<div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
					{filteredApps.map((app) => (
						<ApplicationCard key={app.id} app={app} />
					))}
				</div>
			) : (
				<ApplicationsDataTable columns={columns} data={filteredApps} />
			)}
		</PageContainer>
	);
}
