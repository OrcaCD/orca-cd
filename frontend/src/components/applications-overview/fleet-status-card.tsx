import { CircleCheck } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import type { ApplicationListItem } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";
import {
	countFleetCategories,
	fleetCategories,
	fleetCategoryConfig,
	type FleetCategory,
} from "./fleet";

interface FleetStatusCardProps {
	apps: ApplicationListItem[];
	filter: FleetCategory | null;
	onFilterChange: (filter: FleetCategory | null) => void;
}

export function FleetStatusCard({ apps, filter, onFilterChange }: FleetStatusCardProps) {
	const counts = countFleetCategories(apps);
	const total = apps.length;
	const allOk = counts.ok === total;
	const visibleCategories = fleetCategories.filter((category) => counts[category] > 0);

	return (
		<Card className="md:col-span-2 xl:col-span-1">
			<CardHeader>
				<CardTitle>{m.applicationsFleetTitle()}</CardTitle>
				<CardDescription>{m.applicationsFleetCount({ count: total })}</CardDescription>
			</CardHeader>
			<CardContent className="space-y-3">
				<div
					className={cn(
						"flex items-center gap-2 font-medium",
						allOk ? "text-emerald-400" : "text-foreground",
					)}
				>
					{allOk ? <CircleCheck className="size-4" /> : null}
					{m.applicationsFleetHealthyCount({ healthy: counts.ok, total })}
				</div>

				<div className="flex h-2 gap-0.5 overflow-hidden rounded-full bg-muted" aria-hidden="true">
					{[...visibleCategories].reverse().map((category) => (
						<div
							key={category}
							className={cn(
								"h-full transition-[flex-grow,opacity]",
								fleetCategoryConfig[category].barClass,
								filter && filter !== category && "opacity-30",
							)}
							style={{ flexGrow: counts[category] }}
						/>
					))}
				</div>

				{allOk ? (
					<p className="text-xs text-muted-foreground">{m.applicationsFleetNothingToDo()}</p>
				) : (
					<div className="flex flex-wrap gap-1.5">
						{visibleCategories.map((category) => {
							const active = filter === category;
							const config = fleetCategoryConfig[category];
							return (
								<Badge
									key={category}
									variant={active ? "secondary" : "outline"}
									className={cn("h-6 cursor-pointer px-2.5", active && "ring-1 ring-foreground/30")}
									render={
										<button
											type="button"
											aria-pressed={active}
											onClick={() => onFilterChange(active ? null : category)}
										/>
									}
								>
									<span className={cn("size-1.5 rounded-full", config.dotClass)} />
									{counts[category]} {config.label()}
								</Badge>
							);
						})}
					</div>
				)}
			</CardContent>
		</Card>
	);
}
