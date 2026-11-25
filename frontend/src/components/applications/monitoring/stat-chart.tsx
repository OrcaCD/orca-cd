import { Area, AreaChart, XAxis, YAxis } from "recharts";
import {
	ChartContainer,
	ChartTooltip,
	ChartTooltipContent,
	type ChartConfig,
} from "@/components/ui/chart";
import {
	STATS_HISTORY,
	STATS_INTERVAL_MS,
	chartDomain,
	type StatsPoint,
} from "@/lib/container-stats";
import { getLocale } from "@/lib/paraglide/runtime";

export interface ChartSeries {
	key: string;
	label: string;
	color: string;
	value: (point: StatsPoint) => number | null;
}

type ChartRow = Record<string, number | null>;

const WINDOW_MS = (STATS_HISTORY - 1) * STATS_INTERVAL_MS;

export function StatChart({
	points,
	series,
	formatValue,
}: {
	points: StatsPoint[];
	series: ChartSeries[];
	formatValue: (value: number) => string;
}) {
	const config: ChartConfig = Object.fromEntries(
		series.map((entry) => [entry.key, { label: entry.label, color: entry.color }]),
	);
	const rows: ChartRow[] = points.map((point) => ({
		time: point.timestampMs,
		...Object.fromEntries(series.map((entry) => [entry.key, entry.value(point)])),
	}));
	const values = rows.flatMap((row) =>
		series.map((entry) => row[entry.key]).filter((value) => value !== null),
	);
	const latest = points.at(-1)?.timestampMs ?? WINDOW_MS;

	return (
		<ChartContainer config={config} className="aspect-auto h-12 w-full">
			<AreaChart data={rows} margin={{ top: 2, right: 0, bottom: 2, left: 0 }}>
				<XAxis dataKey="time" type="number" domain={[latest - WINDOW_MS, latest]} hide />
				<YAxis domain={chartDomain(values)} hide />
				<ChartTooltip
					content={
						<ChartTooltipContent
							indicator="line"
							labelFormatter={(_, payload) =>
								new Date(Number(payload?.[0]?.payload?.time)).toLocaleTimeString(getLocale())
							}
							formatter={(value, _name, item) => (
								<>
									<div
										className="w-1 shrink-0 rounded-[2px]"
										style={{ backgroundColor: item.color }}
									/>
									<div className="flex flex-1 items-center justify-between gap-4 leading-none">
										<span className="text-muted-foreground">
											{config[String(item.dataKey)]?.label}
										</span>
										<span className="font-mono font-medium text-foreground tabular-nums">
											{formatValue(Number(value))}
										</span>
									</div>
								</>
							)}
						/>
					}
				/>
				{series.map((entry) => (
					<Area
						key={entry.key}
						dataKey={entry.key}
						type="monotone"
						stroke={`var(--color-${entry.key})`}
						fill={`var(--color-${entry.key})`}
						fillOpacity={0.15}
						strokeWidth={1.5}
						dot={false}
						isAnimationActive={false}
					/>
				))}
			</AreaChart>
		</ChartContainer>
	);
}
