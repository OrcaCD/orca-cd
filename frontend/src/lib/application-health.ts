import useSWR from "swr";
import { fetcher } from "./api";
import { HealthStatus } from "./applications";
import type { ApplicationEventType } from "./application-events";
import { m } from "./paraglide/messages";

export const healthRanges = ["24h", "7d", "30d"] as const;
export type HealthRange = (typeof healthRanges)[number];

export enum HealthChangeReason {
	AgentOffline = "agent_offline",
}

export interface ServiceHealth {
	name: string;
	status: HealthStatus;
}

export interface HealthBucket {
	start: string;
	end: string;
	/** null when the bucket lies before the application's first health change. */
	status: HealthStatus | null;
	healthySeconds: number;
	degradedSeconds: number;
	unhealthySeconds: number;
	stoppedSeconds: number;
	noDataSeconds: number;
}

export interface HealthIncident {
	status: HealthStatus;
	startedAt: string;
	endedAt: string | null;
	durationSeconds: number;
	reason?: HealthChangeReason;
	services: ServiceHealth[];
	trigger?: {
		type: ApplicationEventType;
		commitHash?: string;
	};
}

export interface HealthStats {
	range: HealthRange;
	from: string;
	to: string;
	bucketSeconds: number;
	current: { status: HealthStatus; since: string } | null;
	availability: number | null;
	healthySeconds: number;
	downtimeSeconds: number;
	noDataSeconds: number;
	incidentCount: number;
	longestIncidentSeconds: number;
	dataGapCount: number;
	/** Set when the range had too many changes; only the latest ones are included. */
	truncated: boolean;
	buckets: HealthBucket[];
	incidents: HealthIncident[];
}

// Bars move with time, so refresh even without live updates.
const HEALTH_REFRESH_INTERVAL = 60_000;

/**
 * Loads the health statistics. The array key keeps them out of the live
 * application updates, which fire for every change of any application; they
 * are reloaded when the application's health changes instead.
 */
export function useApplicationHealth(
	applicationId: string,
	range: HealthRange,
	currentHealth: HealthStatus | undefined,
) {
	return useSWR<HealthStats>(
		currentHealth ? ["application-health", applicationId, range, currentHealth] : null,
		([, id, healthRange]: [string, string, HealthRange]) =>
			fetcher<HealthStats>(`/applications/${id}/health?range=${healthRange}`),
		{
			refreshInterval: HEALTH_REFRESH_INTERVAL,
			keepPreviousData: true,
		},
	);
}

export const healthStatusConfig: Record<HealthStatus, { label: () => string; className: string }> =
	{
		[HealthStatus.Healthy]: { label: () => m.statusHealthy(), className: "bg-emerald-500" },
		[HealthStatus.Degraded]: { label: () => m.statusDegraded(), className: "bg-yellow-300" },
		[HealthStatus.Unhealthy]: { label: () => m.statusUnhealthy(), className: "bg-amber-500" },
		[HealthStatus.Stopped]: { label: () => m.statusStopped(), className: "bg-red-500" },
		[HealthStatus.Unknown]: {
			label: () => m.healthNoData(),
			className:
				"bg-[repeating-linear-gradient(-45deg,var(--color-zinc-500)_0_1.5px,transparent_1.5px_5px)] ring-1 ring-inset ring-zinc-500/60",
		},
	};

export const healthLegend: HealthStatus[] = [
	HealthStatus.Healthy,
	HealthStatus.Degraded,
	HealthStatus.Unhealthy,
	HealthStatus.Stopped,
	HealthStatus.Unknown,
];

/** Formats seconds as a compact duration with the two largest units, e.g. "1h 30m". */
export function formatHealthDuration(totalSeconds: number): string {
	const seconds = Math.max(0, Math.round(totalSeconds));
	const days = Math.floor(seconds / 86_400);
	const hours = Math.floor((seconds % 86_400) / 3600);
	const minutes = Math.floor((seconds % 3600) / 60);
	const join = (major: string, minor: number, unit: string) =>
		minor > 0 ? `${major} ${minor}${unit}` : major;

	if (days > 0) {
		return join(`${days}d`, hours, "h");
	}
	if (hours > 0) {
		return join(`${hours}h`, minutes, "m");
	}
	if (minutes > 0) {
		return `${minutes}m`;
	}
	return `${seconds}s`;
}

/**
 * Formats the availability with two decimals. It is rounded down so a short
 * incident never shows up as 100%.
 */
export function formatAvailability(availability: number, locale: string): string {
	return new Intl.NumberFormat(locale, {
		style: "percent",
		minimumFractionDigits: 2,
		maximumFractionDigits: 2,
	}).format(Math.floor(availability * 100) / 10_000);
}
