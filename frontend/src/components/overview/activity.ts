import { useFetch } from "@/lib/api";
import type { ApplicationActivity } from "@/lib/applications";

// Sent so the backend buckets deployments into the user's calendar days.
const utcOffset = -new Date().getTimezoneOffset();

export function useApplicationActivity() {
	return useFetch<ApplicationActivity>(`/activity?utcOffset=${utcOffset}`);
}
