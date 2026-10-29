import { Card, CardContent, CardHeader, CardDescription, CardTitle } from "@/components/ui/card";
import {
	formatAvailability,
	formatHealthDuration,
	type HealthStats,
} from "@/lib/application-health";
import { m } from "@/lib/paraglide/messages";
import { getLocale } from "@/lib/paraglide/runtime";

function StatCard({ title, value, hint }: { title: string; value: string; hint: string }) {
	return (
		<Card>
			<CardHeader>
				<CardDescription>{title}</CardDescription>
				<CardTitle className="text-2xl font-semibold tabular-nums">{value}</CardTitle>
			</CardHeader>
			<CardContent className="text-xs text-muted-foreground">{hint}</CardContent>
		</Card>
	);
}

export function HealthStatCards({ stats }: { stats: HealthStats }) {
	return (
		<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
			<StatCard
				title={m.healthAvailability()}
				value={
					stats.availability === null ? "–" : formatAvailability(stats.availability, getLocale())
				}
				hint={m.healthLastRange({ range: stats.range })}
			/>
			<StatCard
				title={m.healthDowntime()}
				value={formatHealthDuration(stats.downtimeSeconds)}
				hint={m.healthDowntimeHint()}
			/>
			<StatCard
				title={m.healthIncidents()}
				value={String(stats.incidentCount)}
				hint={
					stats.incidentCount > 0
						? m.healthLongestIncident({
								duration: formatHealthDuration(stats.longestIncidentSeconds),
							})
						: m.healthNoIncidentsHint()
				}
			/>
			<StatCard
				title={m.healthNoData()}
				value={formatHealthDuration(stats.noDataSeconds)}
				hint={m.healthNoDataHint()}
			/>
		</div>
	);
}
