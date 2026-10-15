import { Link } from "@tanstack/react-router";
import { CircleCheck } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import type { ApplicationListItem } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";
import { countFleetCategories, fleetCategories, fleetCategoryConfig } from "@/lib/fleet";

export function FleetStatusCard({ apps }: { apps: ApplicationListItem[] }) {
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
								"h-full transition-[flex-grow]",
								fleetCategoryConfig[category].barClass,
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
							const config = fleetCategoryConfig[category];
							return (
								<Badge
									key={category}
									variant="outline"
									className="h-6 px-2.5"
									render={<Link to="/applications" search={{ status: category }} />}
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
