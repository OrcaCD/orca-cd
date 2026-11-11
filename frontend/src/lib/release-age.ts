import { fetcher } from "@/lib/api";
import type { Application } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { getLocale } from "@/lib/paraglide/runtime";

export const hourInSeconds = 60 * 60;
export const dayInSeconds = 24 * hourInSeconds;
export const maxReleaseAgeSeconds = 365 * dayInSeconds;

// The label that overrides the minimum release age of a service in the compose file.
export const releaseAgeLabel = "orca-cd.minimum-release-age";

export interface GlobalReleaseAgeSettings {
	minimumSeconds: number;
	allowedRegistries: string[];
}

export enum ImageUpdateStatus {
	UpToDate = "up_to_date",
	Waiting = "waiting",
	Blocked = "blocked",
	Updated = "updated",
	Failed = "failed",
}

// Where the minimum age of a service comes from, from strongest to weakest.
export type ReleaseAgeSource = "label" | "service" | "application" | "global";

export interface ImageUpdate {
	service: string;
	image: string;
	status: ImageUpdateStatus;
	currentDigest: string;
	currentVersion: string;
	newDigest: string;
	newVersion: string;
	pushedAt: string | null;
	// firstSeen means pushedAt is when the agent first saw the image, as the registry has no push time.
	firstSeen: boolean;
	eligibleAt: string | null;
	minimumAgeSeconds: number;
	ageSource: ReleaseAgeSource;
	reason: string;
}

export interface ImageUpdates {
	checkedAt: string | null;
	images: ImageUpdate[];
}

export function updateGlobalReleaseAgeSettings(
	data: GlobalReleaseAgeSettings,
): Promise<GlobalReleaseAgeSettings> {
	return fetcher<GlobalReleaseAgeSettings>("/admin/settings/release-age", "PUT", data);
}

// effectiveMinimumSeconds is the minimum release age of the application without service overrides.
export function effectiveMinimumSeconds(
	application: Application | undefined,
	global: GlobalReleaseAgeSettings | undefined,
): number {
	return application?.releaseAge.minimumSeconds ?? global?.minimumSeconds ?? 0;
}

// formatReleaseAge formats an age like "3 days" or "12 hours", 0 is "Off".
export function formatReleaseAge(seconds: number): string {
	if (seconds <= 0) {
		return m.releaseAgeOff();
	}
	const [value, unit] =
		seconds % dayInSeconds === 0
			? [seconds / dayInSeconds, "day"]
			: seconds % hourInSeconds === 0
				? [seconds / hourInSeconds, "hour"]
				: [Math.round(seconds / 60), "minute"];
	return new Intl.NumberFormat(getLocale(), {
		style: "unit",
		unit,
		unitDisplay: "long",
	}).format(value);
}

// formatRemaining formats a wait like "2d 10h", "5h 3m" or "12m".
export function formatRemaining(ms: number): string {
	const minutes = Math.max(1, Math.ceil(ms / 60_000));
	const days = Math.floor(minutes / (24 * 60));
	const hours = Math.floor((minutes % (24 * 60)) / 60);
	if (days > 0) {
		return `${days}d ${hours}h`;
	}
	if (hours > 0) {
		return `${hours}h ${minutes % 60}m`;
	}
	return `${minutes}m`;
}

// shortDigest shortens "sha256:abc…" to its first 12 hex characters, like docker does.
export function shortDigest(digest: string): string {
	return digest.replace(/^sha256:/, "").slice(0, 12);
}
