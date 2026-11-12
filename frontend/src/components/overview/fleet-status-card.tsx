import { Link } from "@tanstack/react-router";
import { CircleCheck, Container } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import type { ApplicationListItem } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";
import {
	countFleetCategories,
	fleetCategories,
	fleetCategoryConfig,
	getFleetCategory,
} from "@/lib/fleet";
import { Metric, OverviewCardHeader } from "./overview-card";

// Up to this many applications every one gets its own segment, beyond that the bar shows proportions.
const maxSegments = 48;

export function FleetStatusCard({ apps }: { apps: ApplicationListItem[] }) {
	const counts = countFleetCategories(apps);
	const total = apps.length;
	const allOk = counts.ok === total;
	const visibleCategories = fleetCategories.filter((category) => counts[category] > 0);

	return (
		<Card className="md:col-span-2 xl:col-span-1">
			<OverviewCardHeader
				icon={Container}
				title={m.applicationsFleetTitle()}
				description={m.applicationsFleetCount({ count: total })}
			/>
			<CardContent className="flex flex-1 flex-col gap-4">
				<Metric
					value={counts.ok}
					label={m.applicationsFleetHealthyCount({ total })}
					tone={allOk ? "ok" : counts.ok === 0 ? "error" : "warning"}
				/>

				{/* Like the deployments chart, the breakdown sits at the bottom so the metrics of a row line up. */}
				{total <= maxSegments ? (
					<div className="mt-auto flex h-2 gap-1" aria-hidden="true">
						{[...apps]
							.map((app) => ({ app, category: getFleetCategory(app) }))
							.sort(
								(a, b) =>
									fleetCategories.indexOf(a.category) - fleetCategories.indexOf(b.category) ||
									a.app.name.localeCompare(b.app.name),
							)
							.map(({ app, category }) => (
								<div
									key={app.id}
									title={app.name}
									className={cn(
										"flex-1 rounded-full transition-colors",
										fleetCategoryConfig[category].barClass,
									)}
								/>
							))}
					</div>
				) : (
					<div className="mt-auto flex h-2 gap-1" aria-hidden="true">
						{visibleCategories.map((category) => (
							<div
								key={category}
								className={cn(
									"h-full rounded-full transition-[flex-grow]",
									fleetCategoryConfig[category].barClass,
								)}
								style={{ flexGrow: counts[category] }}
							/>
						))}
					</div>
				)}

				{allOk ? (
					<p className="flex items-center gap-1.5 text-xs text-emerald-700 dark:text-emerald-400">
						<CircleCheck className="size-3.5" />
						{m.applicationsFleetNothingToDo()}
					</p>
				) : (
					<ul className="-mx-2 flex flex-wrap gap-x-1">
						{visibleCategories.map((category) => {
							const config = fleetCategoryConfig[category];
							return (
								<li key={category}>
									<Link
										to="/applications"
										search={{ status: category }}
										className="flex h-8 items-center gap-2 rounded-md px-2 text-sm transition-colors hover:bg-muted"
									>
										<span className={cn("size-2 shrink-0 rounded-full", config.dotClass)} />
										<span className="font-medium tabular-nums">{counts[category]}</span>
										<span className="text-muted-foreground">{config.label()}</span>
									</Link>
								</li>
							);
						})}
					</ul>
				)}
			</CardContent>
		</Card>
	);
}
