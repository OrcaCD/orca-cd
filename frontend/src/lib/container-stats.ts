import { getLocale } from "./paraglide/runtime";

export const STATS_HISTORY = 150;
export const STATS_INTERVAL_MS = 2_000;

const MIN_RELATIVE_SPAN = 0.02;

export function chartDomain(values: number[]): [number, number] {
	if (values.length === 0) {
		return [0, 1];
	}
	const min = Math.min(...values);
	const max = Math.max(...values);
	if (max <= 0) {
		return [0, 1];
	}
	const span = Math.max(max - min, max * MIN_RELATIVE_SPAN);
	const low = (min + max) / 2 - span / 2;
	return [low, low + span];
}

export interface StatsSample {
	timestampMs: number;
	cpuPercent: number;
	memUsageBytes: number;
	memLimitBytes: number;
	netRxBytes: number;
	netTxBytes: number;
	pids: number;
	onlineCpus: number;
}

export interface StatsPoint extends StatsSample {
	rxRate: number | null;
	txRate: number | null;
}

function counterRate(current: number, before: number | undefined, seconds: number): number | null {
	if (before === undefined || seconds <= 0 || current < before) {
		return null;
	}
	return (current - before) / seconds;
}

export function appendSample(points: StatsPoint[], sample: StatsSample): StatsPoint[] {
	const previous = points.at(-1);
	const seconds = previous ? (sample.timestampMs - previous.timestampMs) / 1000 : 0;
	const point: StatsPoint = {
		...sample,
		rxRate: counterRate(sample.netRxBytes, previous?.netRxBytes, seconds),
		txRate: counterRate(sample.netTxBytes, previous?.netTxBytes, seconds),
	};
	return [...points, point].slice(-STATS_HISTORY);
}

export function cpuShare(sample: StatsSample, cpuLimit: number): { percent: number; cpus: number } {
	const cpus = cpuLimit > 0 ? cpuLimit : Math.max(sample.onlineCpus, 1);
	return { percent: sample.cpuPercent / cpus, cpus };
}

export function formatPercent(percent: number): string {
	return new Intl.NumberFormat(getLocale(), {
		style: "percent",
		maximumFractionDigits: 1,
	}).format(percent / 100);
}

export function formatCpuCount(cpus: number): string {
	return new Intl.NumberFormat(getLocale(), { maximumFractionDigits: 2 }).format(cpus);
}
