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
	// The global minimum raised the age of its source.
	minimumAgeEnforced: boolean;
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

// effectiveMinimumSeconds is the minimum release age of the application without service
// overrides. The global minimum is also the least one.
export function effectiveMinimumSeconds(
	application: Application,
	global: GlobalReleaseAgeSettings,
): number {
	return Math.max(application.releaseAge.minimumSeconds ?? 0, global.minimumSeconds);
}

// usesGlobalMinimum reports whether the application uses the global minimum release age,
// because it sets none or a lower one.
export function usesGlobalMinimum(
	application: Application,
	global: GlobalReleaseAgeSettings,
): boolean {
	const own = application.releaseAge.minimumSeconds;
	return own === null || own < global.minimumSeconds;
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

// shortDigest shortens "sha256:abc…" to its first 12 hex characters, like docker does.
export function shortDigest(digest: string): string {
	return digest.replace(/^sha256:/, "").slice(0, 12);
}
