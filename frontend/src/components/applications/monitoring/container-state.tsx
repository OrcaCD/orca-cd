import { Badge } from "@/components/ui/badge";
import { useNow } from "@/hooks/use-now";
import type { ContainerInfo } from "@/lib/application-containers";
import { m } from "@/lib/paraglide/messages";
import { cn, formatDuration, formatRelativeTime } from "@/lib/utils";

type StateTone = "good" | "warn" | "bad" | "neutral";

const stateConfig: Record<string, { label: () => string; tone: StateTone }> = {
	running: { label: () => m.monitoringStateRunning(), tone: "good" },
	paused: { label: () => m.monitoringStatePaused(), tone: "warn" },
	restarting: { label: () => m.monitoringStateRestarting(), tone: "warn" },
	exited: { label: () => m.monitoringStateExited(), tone: "bad" },
	dead: { label: () => m.monitoringStateDead(), tone: "bad" },
	created: { label: () => m.monitoringStateCreated(), tone: "neutral" },
	removing: { label: () => m.monitoringStateRemoving(), tone: "neutral" },
};

const toneClasses: Record<StateTone, { badge: string; dot: string }> = {
	good: {
		badge: "bg-emerald-500/20 text-emerald-700 dark:text-emerald-400 border-emerald-500/30",
		dot: "bg-emerald-500 dark:bg-emerald-400",
	},
	warn: {
		badge: "bg-amber-500/20 text-amber-700 dark:text-amber-400 border-amber-500/30",
		dot: "bg-amber-500 dark:bg-amber-400",
	},
	bad: {
		badge: "bg-red-500/20 text-red-700 dark:text-red-400 border-red-500/30",
		dot: "bg-red-500 dark:bg-red-400",
	},
	neutral: {
		badge: "bg-zinc-500/20 text-zinc-600 dark:text-zinc-400 border-zinc-500/30",
		dot: "bg-zinc-400",
	},
};

export function containerStateDotClass(state: string): string {
	return toneClasses[stateConfig[state]?.tone ?? "neutral"].dot;
}

export function ContainerStatePill({ container }: { container: ContainerInfo }) {
	const now = useNow();
	const config = stateConfig[container.state];
	const tone = toneClasses[config?.tone ?? "neutral"];
	const label = config ? config.label() : container.state;

	let detail: string | null = null;
	if (container.state === "running" && container.startedAt) {
		detail = m.monitoringUptime({
			duration: formatDuration(now - new Date(container.startedAt).getTime()),
		});
	} else if (container.state === "exited" && container.finishedAt) {
		detail = formatRelativeTime(container.finishedAt, now);
	}

	return (
		<Badge variant="outline" className={cn("px-2.5 py-1", tone.badge)}>
			<span className={cn("h-1.5 w-1.5 rounded-full", tone.dot)} />
			{detail ? `${label} · ${detail}` : label}
		</Badge>
	);
}
